// Typed product-event emitter (05 §2.6, D-50). The event names and props come from
// shared/telemetry/events/<domain>.yaml through the generated ./gen/events.ts, so an unknown event or a
// prop outside its enum is a type error. Clients post only client events; facts are emitted by the server.
// Free text is never a prop. Nothing is sent from local dev or tests.

import { endpointAllowed, sendJson, type SendEnv } from './allow';
import type { ClientEventName, EventProps } from './gen/events';
import { anonId, newUlid, sessionId } from './ids';

export type EmitterApp = 'customer' | 'business' | 'admin' | 'site';

export interface EmitterConfig {
  app: EmitterApp;
  env: SendEnv;
  /** Full URL of `POST /v1/events`. Unset means events are off. */
  endpoint?: string | undefined;
  /** `app@gitsha`; only the part after `@` is sent as the app version. */
  release?: string | undefined;
  platform?: 'web' | 'android' | 'ios' | 'desktop' | 'other' | undefined;
  /** Send from local dev anyway (opt-in, for working on the events pipeline itself). */
  forceLocal?: boolean | undefined;
  /** Milliseconds before a queued event is flushed. Default 5000. */
  flushMs?: number | undefined;
  /** Clock and id sources, for tests. */
  now?: (() => number) | undefined;
}

type EmitArgs<N extends ClientEventName> = Record<string, never> extends EventProps[N]
  ? [props?: EventProps[N], opts?: EmitOptions]
  : [props: EventProps[N], opts?: EmitOptions];

export interface EmitOptions {
  /** H3 resolution-8 cell (15 hex digits); never coordinates (D-59). */
  geoCell?: string;
}

export interface Emitter {
  readonly enabled: boolean;
  emit<N extends ClientEventName>(name: N, ...args: EmitArgs<N>): void;
  /** Sends the queue now. Returns the number of events sent. */
  flush(): number;
  shutdown(): void;
}

interface QueuedEvent {
  id: string;
  name: string;
  t: string;
  props?: Record<string, unknown>;
  geoCell?: string;
}

const MAX_BATCH = 50;
const GEO_CELL = /^[0-9a-f]{15}$/;

const off: Emitter = { enabled: false, emit: () => undefined, flush: () => 0, shutdown: () => undefined };

export function createEmitter(config: EmitterConfig): Emitter {
  if (typeof window === 'undefined' || !endpointAllowed(config.env, config.endpoint, config.forceLocal)) return off;
  const endpoint = config.endpoint as string;
  const now = config.now ?? Date.now;
  const version = (config.release ?? '').split('@').pop() ?? '';
  const queue: QueuedEvent[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;

  const flush = (): number => {
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
    if (queue.length === 0) return 0;
    const batch = queue.splice(0, MAX_BATCH);
    const body: Record<string, unknown> = {
      app: config.app,
      platform: config.platform ?? 'web',
      anonId: anonId(),
      sessionId: sessionId(),
      events: batch,
    };
    if (/^[A-Za-z0-9._+-]{1,40}$/.test(version)) body['appVersion'] = version;
    sendJson(endpoint, JSON.stringify(body));
    if (queue.length > 0) return batch.length + flush();
    return batch.length;
  };

  const onHide = (): void => {
    if (document.visibilityState === 'hidden') flush();
  };
  document.addEventListener('visibilitychange', onHide);

  return {
    enabled: true,
    emit(name, ...args) {
      const [props, opts] = args as unknown as [Record<string, unknown> | undefined, EmitOptions | undefined];
      const e: QueuedEvent = { id: newUlid(now()), name, t: new Date(now()).toISOString() };
      if (props && Object.keys(props).length > 0) e.props = props;
      if (opts?.geoCell && GEO_CELL.test(opts.geoCell)) e.geoCell = opts.geoCell;
      queue.push(e);
      if (queue.length >= MAX_BATCH) flush();
      else timer ??= setTimeout(flush, config.flushMs ?? 5000);
    },
    flush,
    shutdown() {
      document.removeEventListener('visibilitychange', onHide);
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
    },
  };
}
