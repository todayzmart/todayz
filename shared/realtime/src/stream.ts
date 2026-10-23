import { createSseParser } from './parse';

export interface RealtimeEvent {
  type: string;
  id: string;
  version: number;
}

export type RealtimeStatus = 'connecting' | 'open' | 'polling' | 'stopped';

export interface RealtimeTimers {
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
  now(): number;
  /** Jitter source in [0, 1). Defaults to Math.random. */
  random?(): number;
}

export interface RealtimeVisibility {
  isVisible(): boolean;
  subscribe(cb: () => void): () => void;
}

export interface RealtimeStreamOptions {
  url: string;
  getToken(): string | null;
  /** Refresh the access token through the session. Resolves true when a new token is available. */
  refresh(): Promise<boolean>;
  fetch?: typeof fetch;
  onEvent(e: RealtimeEvent): void;
  /** The server could not resume from our cursor: refetch everything this stream covers. */
  onReset(): void;
  onStatus?(s: RealtimeStatus): void;
  /** Fallback refetch while the stream is down (after 3 consecutive failures). */
  poll?(): void | Promise<void>;
  visibility?: RealtimeVisibility;
  timers?: RealtimeTimers;
}

export interface RealtimeStream {
  stop(): void;
}

export const DEFAULT_RETRY_MS = 3000;
export const MAX_BACKOFF_MS = 30_000;
export const POLL_INTERVAL_MS = 15_000;
export const WATCHDOG_MS = 60_000;
export const FAILURES_BEFORE_POLLING = 3;

const realTimers: RealtimeTimers = {
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  now: () => Date.now(),
};

/** Seconds form of Retry-After only; an HTTP date is ignored. */
function retryAfterMs(res: Response): number {
  const raw = res.headers.get('Retry-After');
  if (!raw || !/^[0-9]+$/.test(raw.trim())) return 0;
  return Math.min(Number(raw) * 1000, MAX_BACKOFF_MS);
}

function parseEvent(event: string, data: string, id: string): RealtimeEvent {
  let type = event;
  let evId = id;
  let version = 0;
  try {
    const j = JSON.parse(data) as { type?: unknown; id?: unknown; version?: unknown } | null;
    if (j && typeof j === 'object') {
      if (typeof j.type === 'string') type = j.type;
      if (typeof j.id === 'string') evId = j.id;
      if (typeof j.version === 'number') version = j.version;
    }
  } catch {
    // Keep the frame-level values.
  }
  return { type, id: evId, version };
}

/**
 * Streaming-fetch SSE client (EventSource cannot send Authorization). See 02-architecture §5 / D-98.
 * A server `reconnect` frame is a normal handover, not a failure. Three consecutive failures switch
 * to polling while the stream keeps retrying in the background.
 */
export function createRealtimeStream(opts: RealtimeStreamOptions): RealtimeStream {
  const timers = opts.timers ?? realTimers;
  const doFetch = opts.fetch ?? ((...a: Parameters<typeof fetch>) => fetch(...a));
  const random = timers.random ?? Math.random;

  let stopped = false;
  let lastId: string | null = null;
  let serverRetry = DEFAULT_RETRY_MS;
  let failures = 0;
  let polling = false;
  let controller: AbortController | null = null;
  let reconnectTimer: unknown = null;
  let watchdogTimer: unknown = null;
  let pollTimer: unknown = null;
  let unsubVisibility: (() => void) | null = null;
  let generation = 0;
  let lastStatus: RealtimeStatus | null = null;

  function status(s: RealtimeStatus): void {
    if (s === lastStatus) return;
    lastStatus = s;
    opts.onStatus?.(s);
  }

  function visible(): boolean {
    return opts.visibility ? opts.visibility.isVisible() : true;
  }

  function runPoll(): void {
    if (!opts.poll) return;
    try {
      const r = opts.poll();
      if (r && typeof (r as Promise<void>).catch === 'function') (r as Promise<void>).catch(() => undefined);
    } catch {
      // A failing poll must not break the loop.
    }
  }

  function clearPollTimer(): void {
    if (pollTimer !== null) timers.clearTimeout(pollTimer);
    pollTimer = null;
  }

  function schedulePoll(): void {
    clearPollTimer();
    if (!polling || stopped || !visible()) return;
    pollTimer = timers.setTimeout(() => {
      pollTimer = null;
      if (!polling || stopped) return;
      if (visible()) runPoll();
      schedulePoll();
    }, POLL_INTERVAL_MS);
  }

  function startPolling(): void {
    if (polling) return;
    polling = true;
    status('polling');
    if (visible()) runPoll();
    schedulePoll();
  }

  function stopPolling(): void {
    polling = false;
    clearPollTimer();
  }

  function clearWatchdog(): void {
    if (watchdogTimer !== null) timers.clearTimeout(watchdogTimer);
    watchdogTimer = null;
  }

  function armWatchdog(ac: AbortController): void {
    clearWatchdog();
    watchdogTimer = timers.setTimeout(() => {
      watchdogTimer = null;
      ac.abort();
    }, WATCHDOG_MS);
  }

  function schedule(delay: number): void {
    if (stopped) return;
    if (reconnectTimer !== null) timers.clearTimeout(reconnectTimer);
    reconnectTimer = timers.setTimeout(() => {
      reconnectTimer = null;
      void connect(false);
    }, delay);
  }

  function failure(minDelay = 0): void {
    if (stopped) return;
    failures++;
    if (failures >= FAILURES_BEFORE_POLLING) startPolling();
    const backoff = Math.min(serverRetry * 2 ** (failures - 1), MAX_BACKOFF_MS);
    const jittered = backoff * (0.5 + random() * 0.5);
    schedule(Math.max(jittered, minDelay));
  }

  async function connect(authRetried: boolean): Promise<void> {
    if (stopped) return;
    const gen = ++generation;
    const ac = new AbortController();
    controller = ac;
    if (!polling) status('connecting');

    let reconnectRequested = false;
    const stale = (): boolean => stopped || gen !== generation;

    try {
      const headers: Record<string, string> = { Accept: 'text/event-stream' };
      const token = opts.getToken();
      if (token) headers.Authorization = `Bearer ${token}`;
      if (lastId !== null) headers['Last-Event-ID'] = lastId;

      armWatchdog(ac);
      const res = await doFetch(opts.url, { headers, signal: ac.signal, cache: 'no-store' });
      if (stale()) return;

      if (res.status === 401) {
        clearWatchdog();
        if (authRetried) {
          failure();
          return;
        }
        const ok = await opts.refresh().catch(() => false);
        if (stale()) return;
        if (ok) return connect(true);
        failure();
        return;
      }
      if (res.status === 403 || res.status === 404) {
        clearWatchdog();
        stopPolling();
        stopped = true;
        cleanup();
        status('stopped');
        return;
      }
      if (!res.ok || !res.body) {
        clearWatchdog();
        failure(retryAfterMs(res));
        return;
      }

      const parser = createSseParser({
        onRetry: (ms) => {
          serverRetry = ms;
        },
        onId: (id) => {
          lastId = id;
        },
        onMessage: (m) => {
          switch (m.event) {
            case 'ready':
              failures = 0;
              stopPolling();
              status('open');
              break;
            case 'reset':
              opts.onReset();
              break;
            case 'reconnect':
              reconnectRequested = true;
              break;
            default:
              opts.onEvent(parseEvent(m.event, m.data, m.id));
          }
        },
      });

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      for (;;) {
        const { done, value } = await reader.read();
        if (stale()) {
          void reader.cancel().catch(() => undefined);
          return;
        }
        if (done) break;
        armWatchdog(ac);
        parser.feed(decoder.decode(value, { stream: true }));
        if (stale()) return;
      }
      clearWatchdog();
      if (reconnectRequested) {
        schedule(serverRetry);
      } else {
        failure();
      }
    } catch {
      clearWatchdog();
      if (stale()) return;
      // Watchdog abort, network error and mid-body errors are all failures.
      failure();
    }
  }

  function cleanup(): void {
    generation++;
    clearWatchdog();
    clearPollTimer();
    if (reconnectTimer !== null) timers.clearTimeout(reconnectTimer);
    reconnectTimer = null;
    unsubVisibility?.();
    unsubVisibility = null;
    controller?.abort();
    controller = null;
  }

  if (opts.visibility) {
    unsubVisibility = opts.visibility.subscribe(() => {
      if (!polling || stopped) return;
      if (visible()) {
        runPoll();
        schedulePoll();
      } else {
        clearPollTimer();
      }
    });
  }

  void connect(false);

  return {
    stop() {
      if (stopped) return;
      stopped = true;
      polling = false;
      cleanup();
      status('stopped');
    },
  };
}
