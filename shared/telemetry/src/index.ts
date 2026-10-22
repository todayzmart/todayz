// @todayz/telemetry init stubs (W0-05). The full client (fetch wrapper with traceparent, route timings,
// product events, error capture details) is built later; see docs/plan/05-observability.md §2.4–2.5.
//
// Rules this stub already enforces:
// - Nothing is sent unless an endpoint is configured and the environment allows it. Local dev, tests
//   and builds without config make no network calls at all.
// - No Sentry before Wave 5 (D-17): a DSN is accepted and ignored, there is no Sentry SDK in the bundle.
// - RUM goes only to our own `POST /v1/rum` (same site), with sendBeacon, in batches.

export type AppName = 'customer' | 'business' | 'admin' | 'site';
export type TelemetryEnv = 'local' | 'test' | 'staging' | 'production';

export interface TelemetryConfig {
  app: AppName;
  /** Build environment. `local` and `test` never send anything. */
  env: TelemetryEnv;
  /** `app@gitsha` in real builds. */
  release?: string | undefined;
  /** Full URL of our RUM endpoint (`https://api.todayz.in/v1/rum`). Unset means RUM is off. */
  rumEndpoint?: string | undefined;
  /** Sentry DSN. Ignored until Wave 5 (D-17). */
  sentryDsn?: string | undefined;
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
  readonly sentryEnabled: false;
  /** Records an error for the RUM batch (no-op when RUM is off). */
  captureError(error: unknown, context?: Record<string, string>): void;
  /** Sends queued samples now. Returns the number sent. */
  flush(): number;
  /** Removes listeners; for tests and hot reload. */
  shutdown(): void;
}

const MAX_BATCH = 20;

/** Pure: should RUM send anything for this config? */
export function rumAllowed(config: TelemetryConfig): boolean {
  if (!config.rumEndpoint) return false;
  if (config.env === 'test') return false;
  if (config.env === 'local' && !config.forceLocalRum) return false;
  try {
    const { protocol } = new URL(config.rumEndpoint);
    return protocol === 'https:' || protocol === 'http:';
  } catch {
    return false;
  }
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
  };
}

const noop: Telemetry = {
  rumEnabled: false,
  sentryEnabled: false,
  captureError: () => undefined,
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

export function initTelemetry(config: TelemetryConfig): Telemetry {
  if (typeof window === 'undefined' || !rumAllowed(config)) return noop;
  const endpoint = config.rumEndpoint as string;
  const queue: RumSample[] = [];
  const route = (): string => routeTemplate(window.location.pathname, config.routeId?.());

  const flush = (): number => {
    if (queue.length === 0) return 0;
    const batch = queue.splice(0, queue.length);
    const body = JSON.stringify({ app: config.app, release: config.release ?? 'dev', samples: batch });
    const blob = new Blob([body], { type: 'application/json' });
    if (!navigator.sendBeacon?.(endpoint, blob)) {
      void fetch(endpoint, { method: 'POST', body, keepalive: true, credentials: 'omit' }).catch(() => undefined);
    }
    return batch.length;
  };
  const push = (sample: RumSample): void => {
    queue.push(sample);
    if (queue.length >= MAX_BATCH) flush();
  };
  const captureError = (error: unknown, context?: Record<string, string>): void => {
    const err = error instanceof Error ? error : new Error(String(error));
    const detail: Record<string, string> = { message: scrub(err.message).slice(0, 300) };
    for (const [k, v] of Object.entries(context ?? {})) detail[k] = scrub(v).slice(0, 300);
    push({ type: 'error', name: scrub(err.name).slice(0, 100), route: route(), detail, t: Date.now() });
  };

  const onError = (e: ErrorEvent): void => captureError(e.error ?? e.message);
  const onRejection = (e: PromiseRejectionEvent): void => captureError(e.reason);
  const onHide = (): void => {
    if (document.visibilityState === 'hidden') flush();
  };
  window.addEventListener('error', onError);
  window.addEventListener('unhandledrejection', onRejection);
  document.addEventListener('visibilitychange', onHide);

  // web-vitals loads lazily so it is never in the initial bundle.
  void import('web-vitals').then(({ onCLS, onFCP, onINP, onLCP, onTTFB }) => {
    const report = (m: { name: string; value: number; rating: string }): void =>
      push({ type: 'vital', name: m.name, value: m.value, route: route(), detail: { rating: m.rating }, t: Date.now() });
    onCLS(report);
    onFCP(report);
    onINP(report);
    onLCP(report);
    onTTFB(report);
  });

  return {
    rumEnabled: true,
    sentryEnabled: false,
    captureError,
    flush,
    shutdown: () => {
      window.removeEventListener('error', onError);
      window.removeEventListener('unhandledrejection', onRejection);
      document.removeEventListener('visibilitychange', onHide);
    },
  };
}
