// @todayz/telemetry (W0-05 stubs, W1-07 client): web vitals, long tasks and route timings in RUM batches,
// a traceparent fetch wrapper, scrubbed error capture, an injectable Sentry seam and the typed product-event
// emitter. See docs/plan/05-observability.md §2.4–2.6.
//
// Rules:
// - Nothing is sent unless an endpoint is configured and the environment allows it. Local dev, tests
//   and builds without config make no network calls at all.
// - No Sentry SDK is bundled (D-17): the app injects one in staging and production with a DSN (`config.sentry`).
// - RUM and events go only to our own `POST /v1/rum` and `POST /v1/events`, with sendBeacon, in batches.

import { endpointAllowed, sendJson, type SendEnv } from './allow';
import { createEmitter, type Emitter } from './emitter';
import { scrubSentryEvent, sentryAllowed, type SentryLike } from './sentry';
import { tracedFetch } from './traceparent';

export { endpointAllowed, sendJson } from './allow';
export { createEmitter, type Emitter, type EmitterConfig, type EmitOptions } from './emitter';
export type { ClientEventName, EventName, EventProps } from './gen/events';
export { anonId, newUlid, sessionId } from './ids';
export { scrubSentryEvent, sentryAllowed, type SentryEventLike, type SentryInitOptions, type SentryLike } from './sentry';
export { newTraceparent, TRACEPARENT_PATTERN, tracedFetch, type TracedFetchOptions } from './traceparent';

export type AppName = 'customer' | 'business' | 'admin' | 'site';
export type TelemetryEnv = SendEnv;

export interface TelemetryConfig {
  app: AppName;
  /** Build environment. `local` and `test` never send anything. */
  env: TelemetryEnv;
  /** `app@gitsha` in real builds. */
  release?: string | undefined;
  /** Full URL of our RUM endpoint (`https://api.todayz.in/v1/rum`). Unset means RUM is off. */
  rumEndpoint?: string | undefined;
  /** Sentry DSN. Used only in staging and production, and only with `sentry` (D-17). */
  sentryDsn?: string | undefined;
  /** The Sentry SDK (`@sentry/browser`), injected by the app in Wave 5. No SDK is bundled here. */
  sentry?: SentryLike | undefined;
  /** Full URL of `POST /v1/events`. Unset means product events are off. */
  eventsEndpoint?: string | undefined;
  /** Origins of our API; calls through `Telemetry.fetch` to them carry a `traceparent`. */
  apiOrigins?: readonly string[] | undefined;
  /** Send local RUM to a local API anyway (opt-in, for working on the RUM pipeline itself). */
  forceLocalRum?: boolean | undefined;
  /** The router's matched route id (`/r/$token`), used as the RUM route label when present. */
  routeId?: (() => string | undefined) | undefined;
}

export interface RumSample {
  type: 'vital' | 'error';
  name: string;
  value?: number;
  route: string;
  detail?: Record<string, string | number>;
  t: number;
}

export interface Telemetry {
  readonly rumEnabled: boolean;
  readonly sentryEnabled: boolean;
  /** Records an error for the RUM batch and Sentry (no-op when both are off). */
  captureError(error: unknown, context?: Record<string, string>): void;
  /** Marks the start of a route change (call from the router's before-navigate hook). */
  routeStart(): void;
  /** Marks the route as rendered and painted; records the `route_change` timing. */
  routeReady(routeId?: string): void;
  /** Queues a typed product event (a no-op when events are off). */
  emit: Emitter['emit'];
  /** `fetch` that adds a `traceparent` to calls to `apiOrigins`. The plain `fetch` when none are set. */
  fetch: typeof fetch;
  /** Sends queued samples now. Returns the number sent. */
  flush(): number;
  /** Removes listeners; for tests and hot reload. */
  shutdown(): void;
}

const MAX_BATCH = 20;

/** Pure: should RUM send anything for this config? */
export function rumAllowed(config: TelemetryConfig): boolean {
  return endpointAllowed(config.env, config.rumEndpoint, config.forceLocalRum);
}

/** Reads the app's Vite env into a config. Missing values leave telemetry off. */
export function configFromEnv(app: AppName, env: Record<string, string | boolean | undefined>): TelemetryConfig {
  const str = (k: string): string | undefined => {
    const v = env[k];
    return typeof v === 'string' && v.length > 0 ? v : undefined;
  };
  const mode = str('VITE_TZ_ENV') ?? (env['MODE'] === 'test' ? 'test' : 'local');
  const known: TelemetryEnv[] = ['local', 'test', 'staging', 'production'];
  return {
    app,
    env: known.includes(mode as TelemetryEnv) ? (mode as TelemetryEnv) : 'local',
    release: str('VITE_TZ_RELEASE'),
    rumEndpoint: str('VITE_RUM_ENDPOINT'),
    sentryDsn: str('VITE_SENTRY_DSN'),
    eventsEndpoint: str('VITE_EVENTS_ENDPOINT'),
    apiOrigins: str('VITE_API_ORIGIN') ? [str('VITE_API_ORIGIN') as string] : undefined,
  };
}

const noop: Telemetry = {
  rumEnabled: false,
  sentryEnabled: false,
  captureError: () => undefined,
  routeStart: () => undefined,
  routeReady: () => undefined,
  emit: () => undefined,
  fetch: (...args) => fetch(...args),
  flush: () => 0,
  shutdown: () => undefined,
};

const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/i;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** A path segment that identifies one thing (an id, a token, a code) rather than a page. */
function isIdSegment(seg: string): boolean {
  if (/^\d+$/.test(seg) || ULID.test(seg) || UUID.test(seg)) return true;
  if (seg.length > 20) return true;
  return seg.length >= 12 && /[a-z]/i.test(seg) && /\d/.test(seg);
}

/**
 * Route label for RUM, bounded and free of ids. Prefer the router's matched route id
 * (`/r/$token`, `/orders/$orderId`), which is a template already. Without one, every segment that
 * looks like an id is replaced with `{id}`: digits, ULIDs, UUIDs, anything longer than 20
 * characters, and mixed letters and digits of 12 or more.
 */
export function routeTemplate(pathname: string, routeId?: string): string {
  if (routeId?.startsWith('/')) return routeId.length > 1 ? routeId.replace(/\/$/, '') : routeId;
  return pathname
    .split('/')
    .map((seg) => (isIdSegment(seg) ? '{id}' : seg))
    .join('/');
}

/**
 * Personal data and secrets that must never leave the device in an error report: JWTs, bearer
 * tokens, emails, Indian phone numbers (+91 / 0 / bare 10-digit mobiles), OTPs and pickup codes
 * (4–8 digits), and long opaque tokens. Order matters: tokens first, so their digits are not
 * half-matched as codes.
 */
const SCRUBBERS: readonly [RegExp, string][] = [
  [/\beyJ[\w-]+\.[\w-]+\.[\w-]*/g, '{jwt}'],
  [/\bBearer\s+[\w.~+/=-]+/gi, 'Bearer {token}'],
  [/[\w.+-]+@[\w-]+(?:\.[\w-]+)+/g, '{email}'],
  [/(?<![\w+])(?:\+91[\s-]?|0)?[6-9]\d{4}[\s-]?\d{5}(?!\d)/g, '{phone}'],
  [/(?<![\w-])(?=[\w-]*\d)(?=[\w-]*[a-z])[\w-]{32,}/gi, '{token}'],
  [/(?<![\w.])\d{9,}(?![\w.])/g, '{number}'],
  [/(?<![\w.])\d{4,8}(?![\w.])/g, '{code}'],
];

/** Removes personal data and secrets from a string before it goes into a RUM sample. */
export function scrub(text: string): string {
  return SCRUBBERS.reduce((out, [pattern, replacement]) => out.replace(pattern, replacement), text);
}

const LONG_TASK_MS = 200;
/** The contract caps `value` at one hour; a larger number is a broken timer, not a measurement. */
const MAX_VALUE_MS = 3_600_000;

type Conn = 'slow-2g' | '2g' | '3g' | '4g' | 'wifi' | 'ethernet' | 'unknown';

/** The Network Information API's effective type, or `unknown` (Safari and Firefox have none). */
function connType(): Conn {
  const c = (navigator as { connection?: { effectiveType?: string; type?: string } }).connection;
  if (c?.type === 'wifi' || c?.type === 'ethernet') return c.type;
  const e = c?.effectiveType;
  return e === 'slow-2g' || e === '2g' || e === '3g' || e === '4g' ? e : 'unknown';
}

export function initTelemetry(config: TelemetryConfig): Telemetry {
  if (typeof window === 'undefined') return noop;
  const rumOn = rumAllowed(config);
  const sentry = sentryAllowed(config.env, config.sentryDsn, config.sentry) ? config.sentry : undefined;
  const emitter = createEmitter({
    app: config.app,
    env: config.env,
    endpoint: config.eventsEndpoint,
    release: config.release,
    forceLocal: config.forceLocalRum,
  });
  if (!rumOn && !sentry && !emitter.enabled) return noop;

  const endpoint = config.rumEndpoint as string;
  const queue: RumSample[] = [];
  const route = (): string => routeTemplate(window.location.pathname, config.routeId?.());

  if (sentry) {
    sentry.init({
      dsn: config.sentryDsn as string,
      environment: config.env,
      release: config.release,
      sendDefaultPii: false,
      beforeSend: (event) => scrubSentryEvent(event, scrub),
    });
  }

  const flush = (): number => {
    if (queue.length === 0) return 0;
    const batch = queue.splice(0, queue.length);
    const body = JSON.stringify({
      app: config.app,
      release: (config.release ?? 'dev').slice(0, 64),
      platform: 'web',
      conn: connType(),
      samples: batch,
    });
    sendJson(endpoint, body);
    return batch.length;
  };
  const push = (sample: RumSample): void => {
    if (!rumOn) return;
    queue.push({ ...sample, route: sample.route.slice(0, 120) });
    if (queue.length >= MAX_BATCH) flush();
  };
  const vital = (name: string, value: number, detail?: Record<string, string | number>): void => {
    if (!Number.isFinite(value)) return;
    const sample: RumSample = { type: 'vital', name, value: Math.min(Math.max(value, 0), MAX_VALUE_MS), route: route(), t: Date.now() };
    if (detail) sample.detail = detail;
    push(sample);
  };
  const captureError = (error: unknown, context?: Record<string, string>): void => {
    const err = error instanceof Error ? error : new Error(String(error));
    const detail: Record<string, string> = { message: scrub(err.message).slice(0, 300) };
    for (const [k, v] of Object.entries(context ?? {})) detail[k] = scrub(v).slice(0, 300);
    push({ type: 'error', name: scrub(err.name).slice(0, 100), route: route(), detail, t: Date.now() });
    try {
      sentry?.captureException(error, { extra: detail });
    } catch {
      // Error reporting must never throw.
    }
  };

  const onError = (e: ErrorEvent): void => captureError(e.error ?? e.message);
  const onRejection = (e: PromiseRejectionEvent): void => captureError(e.reason);
  const onHide = (): void => {
    if (document.visibilityState === 'hidden') flush();
  };
  window.addEventListener('error', onError);
  window.addEventListener('unhandledrejection', onRejection);
  document.addEventListener('visibilitychange', onHide);

  let longTasks: PerformanceObserver | undefined;
  if (rumOn) {
    // web-vitals loads lazily so it is never in the initial bundle.
    void import('web-vitals').then(({ onCLS, onFCP, onINP, onLCP, onTTFB }) => {
      const report = (m: { name: string; value: number; rating: string }): void => vital(m.name, m.value, { rating: m.rating });
      onCLS(report);
      onFCP(report);
      onINP(report);
      onLCP(report);
      onTTFB(report);
    });
    try {
      longTasks = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) if (entry.duration > LONG_TASK_MS) vital('long_task', entry.duration);
      });
      longTasks.observe({ type: 'longtask', buffered: true });
    } catch {
      longTasks = undefined; // not supported (Safari, Firefox)
    }
  }

  let routeStartedAt: number | undefined;
  const routeStart = (): void => {
    routeStartedAt = performance.now();
  };
  const routeReady = (routeId?: string): void => {
    const started = routeStartedAt;
    routeStartedAt = undefined;
    if (started === undefined) return;
    // Two frames after the router says "ready" is when the new route has actually been painted.
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const ms = performance.now() - started;
        if (!Number.isFinite(ms)) return;
        const sample: RumSample = {
          type: 'vital',
          name: 'route_change',
          value: Math.min(ms, MAX_VALUE_MS),
          route: routeTemplate(window.location.pathname, routeId ?? config.routeId?.()),
          t: Date.now(),
        };
        push(sample);
      }),
    );
  };

  const origins = config.apiOrigins ?? [];
  const traced = origins.length > 0 ? tracedFetch((...args) => fetch(...args), { apiOrigins: origins }) : (...args: Parameters<typeof fetch>) => fetch(...args);

  return {
    rumEnabled: rumOn,
    sentryEnabled: Boolean(sentry),
    captureError,
    routeStart,
    routeReady,
    emit: emitter.emit,
    fetch: traced,
    flush: () => flush() + emitter.flush(),
    shutdown: () => {
      window.removeEventListener('error', onError);
      window.removeEventListener('unhandledrejection', onRejection);
      document.removeEventListener('visibilitychange', onHide);
      longTasks?.disconnect();
      emitter.shutdown();
    },
  };
}
