// The session of one app (W1-08; 02 §8, D-14, D-105).
//
// The access token lives in this object's memory and nowhere else: not localStorage, not
// sessionStorage, not a cookie. The refresh token is an httpOnly cookie the browser sends to
// /v1/auth/* only. This module:
//   - runs one refresh at a time, across tabs (Web Locks; a BroadcastChannel carries the fresh
//     token to the other tabs; without Web Locks the in-tab single flight and the grace retry
//     below are the fallback);
//   - refreshes shortly before the token expires, when the tab becomes visible or the network
//     returns, and on a 401 from an API call (the call is retried once);
//   - treats a 401 from refresh as a possible race with another tab (D-105: the server answers
//     401 without revoking for 30 s after a rotation and keeps the cookie): it waits a moment,
//     takes the other tab's token if one arrived, else retries once; a second 401 is signed out;
//   - never sends the bearer to the sign-in routes (api-client does that too).
import { ApiError, configure } from '@todayz/api-client';
import { logout as apiLogout, refreshSession } from '@todayz/api-client/gen/auth/auth';

export type AuthApp = 'customer' | 'business' | 'admin';
export type SessionStatus = 'unknown' | 'signedIn' | 'signedOut';

export interface SessionState {
  status: SessionStatus;
  userId: string | null;
  /** True when the last refresh failed for a network or server reason (not a 401). */
  offline: boolean;
}

export interface TokenGrant {
  accessToken: string;
  /** RFC 3339 UTC. */
  expiresAt: string;
  userId?: string | undefined;
}

type Message = { type: 'token'; accessToken: string; expiresAt: string; userId: string | null } | { type: 'signout' };

/** The slice of BroadcastChannel the session uses (tests pass an in-memory bus). */
export interface Channel {
  postMessage(message: unknown): void;
  addEventListener(type: 'message', listener: (e: { data: unknown }) => void): void;
  close(): void;
}

/** The slice of navigator.locks the session uses. */
export interface Locks {
  request<T>(name: string, callback: () => Promise<T>): Promise<T>;
}

export interface SessionOptions {
  app: AuthApp;
  /** API origin without a trailing slash; '' is same origin. */
  baseUrl: string;
  /** `customer/1.4.0`: sent as X-TZ-Client (AU-16). */
  clientId: string;
  language?: (() => string | null | undefined) | undefined;
  fetch?: typeof globalThis.fetch | undefined;
  now?: (() => number) | undefined;
  /** `null` forces the no-locks fallback; undefined uses navigator.locks when present. */
  locks?: Locks | null | undefined;
  /** `null` disables cross-tab messages; undefined opens a BroadcastChannel when present. */
  channel?: Channel | null | undefined;
  /** Refresh this long before expiry (default 60 s). */
  leadMs?: number | undefined;
  /** How long a 401 from refresh waits for the other tab before one retry (default 400 ms). */
  graceWaitMs?: number | undefined;
}

export interface Session {
  readonly app: AuthApp;
  getState(): SessionState;
  subscribe(listener: () => void): () => void;
  getToken(): string | null;
  /** First load: try the cookie. Resolves when the status is known. */
  start(): Promise<void>;
  /** After OTP verify (or TOTP): keep the token, schedule the refresh, tell the other tabs. */
  signIn(grant: TokenGrant): void;
  /** Revoke this app's session on this device and forget the token everywhere. */
  signOut(): Promise<void>;
  refresh(): Promise<boolean>;
  /** The ETag last seen for a GET path (`/v1/me`), for If-Match. */
  etag(path: string): string | undefined;
  /** fetch for api-client: bearer retry on 401 and ETag capture. */
  authFetch: typeof globalThis.fetch;
  dispose(): void;
}

const AUTH_PATH = /^\/v1\/auth\//;

function pathOf(input: RequestInfo | URL): string {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  try {
    return new URL(raw, 'http://x').pathname;
  } catch {
    return raw.split('?', 1)[0] ?? raw;
  }
}

export function createSession(opts: SessionOptions): Session {
  const { app, clientId } = opts;
  const now = opts.now ?? Date.now;
  const lead = opts.leadMs ?? 60_000;
  const graceWait = opts.graceWaitMs ?? 400;
  const baseFetch = (...a: Parameters<typeof fetch>) => (opts.fetch ?? globalThis.fetch)(...a);
  const locks: Locks | null =
    opts.locks !== undefined ? opts.locks : typeof navigator !== 'undefined' && navigator.locks ? navigator.locks : null;
  let channel: Channel | null = null;
  if (opts.channel !== undefined) channel = opts.channel;
  else if (typeof BroadcastChannel !== 'undefined') channel = new BroadcastChannel(`tz-auth-${app}`);

  let token: string | null = null;
  let expiresAt = 0;
  let state: SessionState = { status: 'unknown', userId: null, offline: false };
  /** Bumped on every token we accept, ours or another tab's. */
  let seq = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let inflight: Promise<boolean> | null = null;
  let disposed = false;
  const listeners = new Set<() => void>();
  const etags = new Map<string, string>();

  const setState = (next: Partial<SessionState>) => {
    state = { ...state, ...next };
    listeners.forEach((l) => l());
  };
  const post = (m: Message) => channel?.postMessage(m);

  const authOptions = () => ({
    credentials: 'include' as const,
    headers: { 'X-TZ-Client': clientId },
  });

  function schedule() {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    if (!token || disposed) return;
    const wait = Math.max(5_000, expiresAt - now() - lead);
    timer = setTimeout(() => void refresh(), wait);
  }

  function accept(grant: TokenGrant, broadcast: boolean) {
    token = grant.accessToken;
    expiresAt = Date.parse(grant.expiresAt);
    seq += 1;
    setState({ status: 'signedIn', userId: grant.userId ?? state.userId, offline: false });
    schedule();
    if (broadcast) post({ type: 'token', accessToken: token, expiresAt: grant.expiresAt, userId: state.userId });
  }

  function forget(broadcast: boolean) {
    token = null;
    expiresAt = 0;
    etags.clear();
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    setState({ status: 'signedOut', userId: null, offline: false });
    if (broadcast) post({ type: 'signout' });
  }

  channel?.addEventListener('message', (e) => {
    const m = e.data as Message | undefined;
    if (!m || typeof m !== 'object') return;
    if (m.type === 'token' && typeof m.accessToken === 'string') {
      accept({ accessToken: m.accessToken, expiresAt: m.expiresAt, userId: m.userId ?? undefined }, false);
    } else if (m.type === 'signout') {
      forget(false);
    }
  });

  const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
  const fresh = () => token !== null && expiresAt - now() > lead;

  async function callRefresh(): Promise<'ok' | 'unauthorized' | 'failed'> {
    try {
      const r = await refreshSession({ app }, authOptions());
      accept(r, true);
      return 'ok';
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return 'unauthorized';
      return 'failed';
    }
  }

  async function runRefresh(startSeq: number): Promise<boolean> {
    // Another tab refreshed while we waited for the lock: use its token.
    if (seq !== startSeq && fresh()) return true;
    let r = await callRefresh();
    if (r === 'unauthorized') {
      // D-105: a 401 within the rotation grace is a race with another tab, not a sign-out.
      const before = seq;
      await sleep(graceWait);
      if (seq !== before && fresh()) return true;
      r = await callRefresh();
    }
    if (r === 'ok') return true;
    if (r === 'unauthorized') forget(true);
    else setState({ offline: true });
    return false;
  }

  function refresh(): Promise<boolean> {
    if (inflight) return inflight;
    const startSeq = seq;
    const run = () => runRefresh(startSeq);
    inflight = (locks ? locks.request(`tz-refresh-${app}`, run) : run()).finally(() => {
      inflight = null;
    });
    return inflight;
  }

  const onWake = () => {
    if (token && expiresAt - now() <= lead) void refresh();
  };
  const onVisible = () => {
    if (typeof document !== 'undefined' && document.visibilityState === 'visible') onWake();
  };
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible);
  if (typeof window !== 'undefined') window.addEventListener('online', onWake);

  const authFetch: typeof globalThis.fetch = async (input, init) => {
    const path = pathOf(input);
    const sent = token;
    let res = await baseFetch(input, init);
    if (AUTH_PATH.test(path)) return res;
    if (res.status === 401 && sent !== null) {
      // The token we sent may already have been replaced by another tab's refresh.
      const ok = token !== null && token !== sent ? true : await refresh();
      if (ok && token) {
        const headers = new Headers(init?.headers);
        headers.set('Authorization', `Bearer ${token}`);
        res = await baseFetch(input, { ...init, headers });
      }
    }
    if (res.ok && (init?.method ?? 'GET').toUpperCase() === 'GET') {
      const tag = res.headers.get('etag');
      if (tag) etags.set(path, tag);
    }
    if (res.ok && (init?.method ?? 'GET').toUpperCase() === 'PATCH') {
      const tag = res.headers.get('etag');
      if (tag) etags.set(path, tag);
    }
    return res;
  };

  configure({
    baseUrl: opts.baseUrl,
    token: () => token,
    fetch: authFetch,
    ...(opts.language ? { language: opts.language } : {}),
  });

  return {
    app,
    getState: () => state,
    subscribe(l) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    getToken: () => token,
    async start() {
      if (state.status !== 'unknown') return;
      const ok = await refresh();
      if (!ok && state.status === 'unknown') setState({ status: 'signedOut' });
    },
    signIn(grant) {
      accept(grant, true);
    },
    async signOut() {
      try {
        await apiLogout({ app }, authOptions());
      } catch {
        // Offline or already revoked: the local session ends either way.
      }
      forget(true);
    },
    refresh,
    etag: (p) => etags.get(p),
    authFetch,
    dispose() {
      disposed = true;
      if (timer !== undefined) clearTimeout(timer);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible);
      if (typeof window !== 'undefined') window.removeEventListener('online', onWake);
      channel?.close();
    },
  };
}
