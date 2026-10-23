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
//   - never sends the bearer to the sign-in routes (api-client does that too);
//   - a cold start that fails for a network or server reason stays `unknown` and retries (online,
//     visibility, backoff); only a final 401 is signed out;
//   - a sign-out bumps an epoch, so a refresh that was in flight cannot sign the person back in, and
//     the cross-tab messages carry a stamp so a late token cannot undo a newer sign-out;
//   - two non-secret hints in localStorage: `hasSession.<app>` (skip the cookie try for visitors who
//     never signed in) and `logoutPending.<app>` (a sign-out that could not reach the server is
//     retried on the next load, before any refresh).
import { ApiError, configure, request } from '@todayz/api-client';
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

type Message =
  | {
      type: 'token';
      accessToken: string;
      expiresAt: string;
      /** Expiry on this device's clock (the sender corrected it with the server's Date header). */
      expiresAtMs?: number;
      userId: string | null;
      at?: number;
    }
  | { type: 'signout'; at?: number };

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
  /** A refresh call that takes longer than this counts as failed (default 15 s). */
  refreshTimeoutMs?: number | undefined;
  /** Where the two hints live; undefined uses localStorage when it works, `null` keeps none. */
  storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null | undefined;
}

export interface Session {
  readonly app: AuthApp;
  getState(): SessionState;
  subscribe(listener: () => void): () => void;
  getToken(): string | null;
  /** First load: try the cookie. Resolves after the first attempt; never rejects. */
  start(): Promise<void>;
  /** After OTP verify (or TOTP): keep the token, schedule the refresh, tell the other tabs. */
  signIn(grant: TokenGrant): void;
  /** Revoke this app's session on this device and forget the token everywhere. */
  signOut(): Promise<void>;
  /**
   * Sign out of every device (`POST /v1/auth/logout-all`, a cookie route). A 401 can mean the cookie
   * was rotated moments ago in another tab: refresh once and retry once. If it still fails the call
   * throws and this tab stays signed in: the caller shows "did not complete".
   */
  signOutEverywhere(): Promise<void>;
  refresh(): Promise<boolean>;
  /**
   * Called when a token of a different user arrives (another tab signed in as someone else): the
   * app clears what it cached for the previous user.
   */
  onUserChange(listener: (previous: string, next: string) => void): () => void;
  /** The ETag last seen for a GET path (`/v1/me`), for If-Match. */
  etag(path: string): string | undefined;
  /** fetch for api-client: bearer retry on 401 and ETag capture. */
  authFetch: typeof globalThis.fetch;
  dispose(): void;
}

const AUTH_PATH = /^\/v1\/auth\//;

/** The refresh never waits less than this, however short the token's life looks. */
const MIN_REFRESH_WAIT_MS = 30_000;
/** Used when the server's expiry is missing or not a date. */
const FALLBACK_LIFETIME_MS = 5 * 60_000;
const RETRY_FIRST_MS = 5_000;
const RETRY_MAX_MS = 60_000;

/** True for a 401 whose problem code is `unauthorized`, or that has no readable problem code. */
async function isPlainUnauthorized(res: Response): Promise<boolean> {
  try {
    const body = (await res.clone().json()) as { code?: unknown } | null;
    const code = body && typeof body === 'object' ? body.code : undefined;
    return code === undefined || code === 'unauthorized';
  } catch {
    return true;
  }
}

function pathOf(input: RequestInfo | URL): string {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  try {
    return new URL(raw, 'http://x').pathname;
  } catch {
    return raw.split('?', 1)[0] ?? raw;
  }
}

/** The `sub` claim of an access token, for the UI only: decoded, never verified. */
export function subOf(accessToken: string): string | null {
  try {
    const part = accessToken.split('.')[1];
    if (!part) return null;
    const b64 = part.replace(/-/g, '+').replace(/_/g, '/');
    const json = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4));
    const sub = (JSON.parse(json) as { sub?: unknown }).sub;
    return typeof sub === 'string' && sub !== '' ? sub : null;
  } catch {
    return null;
  }
}

export function createSession(opts: SessionOptions): Session {
  const { app, clientId } = opts;
  const now = opts.now ?? Date.now;
  const lead = opts.leadMs ?? 60_000;
  const graceWait = opts.graceWaitMs ?? 400;
  const refreshTimeout = opts.refreshTimeoutMs ?? 15_000;
  const hintKey = `todayz.hasSession.${app}`;
  const pendingKey = `todayz.logoutPending.${app}`;
  const store = ((): SessionOptions['storage'] => {
    if (opts.storage !== undefined) return opts.storage;
    try {
      return typeof localStorage !== 'undefined' ? localStorage : null;
    } catch {
      return null;
    }
  })();
  const hint = {
    get: (k: string) => {
      try {
        return store?.getItem(k) === '1';
      } catch {
        return false;
      }
    },
    set: (k: string) => {
      try {
        store?.setItem(k, '1');
      } catch {
        // Private mode: the hint is only an optimisation.
      }
    },
    clear: (k: string) => {
      try {
        store?.removeItem(k);
      } catch {
        // Nothing stored, nothing to clear.
      }
    },
  };
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
  /** Bumped by every sign-out; a refresh that started before it must not sign anyone in. */
  let epoch = 0;
  let lastStamp = 0;
  let lastTokenAt = 0;
  let lastSignoutAt = 0;
  /** Server clock minus device clock, from the Date header of the latest answer. */
  let skew = 0;
  let sub: string | null = null;
  let retryDelay = RETRY_FIRST_MS;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let inflight: Promise<boolean> | null = null;
  let disposed = false;
  const listeners = new Set<() => void>();
  const userListeners = new Set<(previous: string, next: string) => void>();
  const etags = new Map<string, string>();

  const setState = (next: Partial<SessionState>) => {
    state = { ...state, ...next };
    listeners.forEach((l) => l());
  };
  const post = (m: Message) => {
    try {
      channel?.postMessage(m);
    } catch {
      // A closed channel: the other tabs find out on their own refresh.
    }
  };
  /** Strictly increasing within this tab, so two events never share a stamp. */
  const stamp = () => {
    lastStamp = Math.max(now(), lastStamp + 1);
    return lastStamp;
  };

  const authOptions = () => ({
    credentials: 'include' as const,
    headers: { 'X-TZ-Client': clientId },
  });

  function schedule() {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    if (!token || disposed) return;
    const wait = Math.max(MIN_REFRESH_WAIT_MS, expiresAt - now() - lead);
    timer = setTimeout(() => void refresh(), wait);
  }

  /** The grant's expiry on this device's clock; never NaN. */
  function expiryOf(iso: string, ms?: number): number {
    if (typeof ms === 'number' && Number.isFinite(ms)) return ms;
    const server = Date.parse(iso);
    if (!Number.isFinite(server)) return now() + FALLBACK_LIFETIME_MS;
    return server - skew;
  }

  function accept(grant: TokenGrant, broadcast: boolean, expiresAtMs?: number, at?: number) {
    const nextSub = subOf(grant.accessToken);
    const changed = sub !== null && nextSub !== null && sub !== nextSub;
    const previous = sub;
    token = grant.accessToken;
    expiresAt = expiryOf(grant.expiresAt, expiresAtMs);
    sub = nextSub ?? (changed ? null : sub);
    seq += 1;
    lastTokenAt = at ?? stamp();
    if (changed) etags.clear();
    setState({
      status: 'signedIn',
      userId: grant.userId ?? nextSub ?? (changed ? null : state.userId),
      offline: false,
    });
    hint.set(hintKey);
    retryDelay = RETRY_FIRST_MS;
    if (retryTimer !== undefined) clearTimeout(retryTimer);
    retryTimer = undefined;
    schedule();
    if (broadcast) {
      post({
        type: 'token',
        accessToken: token,
        expiresAt: grant.expiresAt,
        expiresAtMs: expiresAt,
        userId: state.userId,
        at: lastTokenAt,
      });
    }
    if (changed && previous !== null && nextSub !== null) userListeners.forEach((l) => l(previous, nextSub));
  }

  function forget(broadcast: boolean, at?: number) {
    epoch += 1;
    token = null;
    sub = null;
    expiresAt = 0;
    etags.clear();
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    if (retryTimer !== undefined) clearTimeout(retryTimer);
    retryTimer = undefined;
    lastSignoutAt = at ?? stamp();
    hint.clear(hintKey);
    setState({ status: 'signedOut', userId: null, offline: false });
    if (broadcast) post({ type: 'signout', at: lastSignoutAt });
  }

  channel?.addEventListener('message', (e) => {
    const m = e.data as Message | undefined;
    if (!m || typeof m !== 'object') return;
    const at = typeof m.at === 'number' && Number.isFinite(m.at) ? m.at : undefined;
    if (m.type === 'token' && typeof m.accessToken === 'string') {
      // A token older than the latest sign-out is a late message, not a new sign-in.
      if (at !== undefined && at < lastSignoutAt) return;
      accept({ accessToken: m.accessToken, expiresAt: m.expiresAt, userId: m.userId ?? undefined }, false, m.expiresAtMs, at);
    } else if (m.type === 'signout') {
      // A sign-out older than the token we hold was answered by a newer sign-in.
      if (at !== undefined && at < lastTokenAt) return;
      forget(false, at);
    }
  });

  const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
  const fresh = () => token !== null && expiresAt - now() > lead;

  type Outcome = 'ok' | 'unauthorized' | 'failed' | 'stale';

  async function callRefresh(): Promise<Outcome> {
    const started = epoch;
    const ctl = new AbortController();
    const abort = setTimeout(() => ctl.abort(), refreshTimeout);
    try {
      const r = await refreshSession({ app }, { ...authOptions(), signal: ctl.signal });
      if (epoch !== started) return 'stale';
      accept(r, true);
      return 'ok';
    } catch (e) {
      if (epoch !== started) return 'stale';
      if (e instanceof ApiError && e.status === 401) return 'unauthorized';
      return 'failed';
    } finally {
      clearTimeout(abort);
    }
  }

  async function runRefresh(startSeq: number): Promise<boolean> {
    try {
      // Another tab refreshed while we waited for the lock: use its token.
      if (seq !== startSeq && fresh()) return true;
      const held = token !== null;
      let r = await callRefresh();
      if (r === 'unauthorized' && held) {
        // D-105: a 401 within the rotation grace is a race with another tab, not a sign-out.
        // Only a tab that held a token can be in that race; a cold start has nothing to wait for.
        const before = seq;
        const started = epoch;
        await sleep(graceWait);
        if (epoch !== started) return false;
        if (seq !== before && fresh()) return true;
        r = await callRefresh();
      }
      if (r === 'ok') return true;
      if (r === 'stale') return false;
      if (r === 'unauthorized') forget(held);
      else {
        setState({ offline: true });
        scheduleRetry();
      }
      return false;
    } catch {
      return false;
    }
  }

  function refresh(): Promise<boolean> {
    if (inflight) return inflight;
    const startSeq = seq;
    let began = false;
    const run = () => {
      began = true;
      return runRefresh(startSeq);
    };
    let p: Promise<boolean>;
    try {
      p = locks ? locks.request(`tz-refresh-${app}`, run) : run();
    } catch {
      p = run();
    }
    // Web Locks refused (insecure context, SecurityError): fall back to the in-tab single flight.
    inflight = p
      .catch(() => (began ? false : run()))
      .catch(() => false)
      .finally(() => {
        inflight = null;
      });
    return inflight;
  }

  /** While the first load could not reach the server, try again with a growing pause. */
  function scheduleRetry() {
    if (state.status !== 'unknown' || disposed || retryTimer !== undefined) return;
    retryTimer = setTimeout(() => {
      retryTimer = undefined;
      void attemptStart();
    }, retryDelay);
    retryDelay = Math.min(retryDelay * 2, RETRY_MAX_MS);
  }

  /** Sends the logout that could not be sent before. True when the server has answered. */
  async function flushLogout(): Promise<boolean> {
    if (!hint.get(pendingKey)) return true;
    try {
      await apiLogout({ app }, authOptions());
      hint.clear(pendingKey);
      return true;
    } catch (e) {
      // Any answer from the server (a 401 included) means the session is gone there.
      if (e instanceof ApiError && e.status < 500) {
        hint.clear(pendingKey);
        return true;
      }
      return false;
    }
  }

  async function attemptStart(): Promise<void> {
    try {
      if (state.status !== 'unknown') return;
      // A sign-out that never reached the server comes before any refresh: do not sign back in.
      if (hint.get(pendingKey)) {
        await flushLogout();
        if (state.status === 'unknown') forget(false);
        return;
      }
      // Visitors who never signed in here cost no request.
      if (!hint.get(hintKey)) {
        forget(false);
        return;
      }
      await refresh();
    } catch {
      // refresh() and flushLogout() do not throw; this keeps start() from ever rejecting.
    }
  }

  const onWake = () => {
    if (disposed) return;
    if (hint.get(pendingKey) && state.status === 'signedOut') void flushLogout();
    if (state.status === 'unknown') {
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      retryTimer = undefined;
      void attemptStart();
    } else if (token && expiresAt - now() <= lead) void refresh();
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
    // The server's clock, so token lifetimes do not depend on the device's clock being right.
    const date = Date.parse(res.headers.get('date') ?? '');
    if (Number.isFinite(date)) skew = date - now();
    // Sign-in, refresh, logout and logout-all are cookie routes: never retried with a new bearer.
    if (AUTH_PATH.test(path)) return res;
    // Only a plain `unauthorized` (or an answer with no problem body) means the bearer is stale.
    // Other 401 codes (`otp_expired`: a wrong or expired step-up code) belong to the caller.
    if (res.status === 401 && sent !== null && (await isPlainUnauthorized(res))) {
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
      await attemptStart();
    },
    signIn(grant) {
      // A new sign-in replaces the old session's cookie, so a logout that was still pending is moot.
      hint.clear(pendingKey);
      accept(grant, true);
    },
    async signOut() {
      epoch += 1;
      hint.set(pendingKey);
      try {
        await apiLogout({ app }, authOptions());
        hint.clear(pendingKey);
      } catch (e) {
        // Offline or already revoked: the local session ends either way. An answer from the server
        // means it is done there; no answer leaves the hint, and the next load retries.
        if (e instanceof ApiError && e.status < 500) hint.clear(pendingKey);
      }
      forget(true);
    },
    async signOutEverywhere() {
      // TODO(W1-01b): swap for the generated client once the contract branch merges.
      const call = () => request('/v1/auth/logout-all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ app }),
      });
      try {
        await call();
      } catch (e) {
        if (!(e instanceof ApiError) || e.status !== 401) throw e;
        if (!(await refresh())) throw e;
        await call();
      }
      forget(true);
    },
    onUserChange(l) {
      userListeners.add(l);
      return () => userListeners.delete(l);
    },
    refresh,
    etag: (p) => etags.get(p),
    authFetch,
    dispose() {
      disposed = true;
      if (timer !== undefined) clearTimeout(timer);
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible);
      if (typeof window !== 'undefined') window.removeEventListener('online', onWake);
      channel?.close();
    },
  };
}
