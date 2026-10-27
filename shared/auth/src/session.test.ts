import { request } from '@todayz/api-client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getDeviceId } from './device';
import { createSession, type Channel, type Locks, type Session } from './session';

interface Call {
  method: string;
  path: string;
  auth: string | null;
  client: string | null;
  credentials: RequestCredentials | undefined;
}

/** A fake API: refresh hands out token-1, token-2, ... unless the test says otherwise. */
function makeServer() {
  const calls: Call[] = [];
  let n = 0;
  const state = {
    refreshStatuses: [] as number[], // consumed per refresh call; empty means 200
    validTokens: new Set<string>(),
    expiresInMs: 15 * 60_000,
    etag: '"v1"',
    refreshDelayMs: 0,
    logoutDelayMs: 0,
  };
  const json = (status: number, body: unknown, extra: Record<string, string> = {}) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json', ...extra },
    });
  const problem = (status: number, code: string) => json(status, { type: 't', title: 't', status, code });
  const fetch: typeof globalThis.fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const path = new URL(url, 'http://x').pathname;
    const headers = new Headers(init?.headers);
    calls.push({
      method: init?.method ?? 'GET',
      path,
      auth: headers.get('authorization'),
      client: headers.get('x-tz-client'),
      credentials: init?.credentials,
    });
    if (path === '/v1/auth/refresh') {
      if (state.refreshDelayMs) await new Promise((r) => setTimeout(r, state.refreshDelayMs));
      const status = state.refreshStatuses.shift() ?? 200;
      if (status === 200) {
        n += 1;
        const token = `token-${n}`;
        state.validTokens.add(token);
        return json(200, {
          accessToken: token,
          tokenType: 'Bearer',
          expiresAt: new Date(Date.now() + state.expiresInMs).toISOString(),
        });
      }
      return status === 401 ? problem(401, 'unauthorized') : problem(status, 'internal');
    }
    if (path === '/v1/auth/logout') {
      if (state.logoutDelayMs) await new Promise((r) => setTimeout(r, state.logoutDelayMs));
      return new Response(null, { status: 204 });
    }
    if (path === '/v1/me') {
      const token = headers.get('authorization')?.replace('Bearer ', '');
      if (!token || !state.validTokens.has(token)) return problem(401, 'unauthorized');
      return json(200, { id: 'u1' }, { etag: state.etag });
    }
    return problem(404, 'not_found');
  };
  return { calls, state, fetch, refreshCalls: () => calls.filter((c) => c.path === '/v1/auth/refresh').length };
}

class Bus {
  private members: { post: (m: unknown) => void }[] = [];
  channel(): Channel {
    const listeners: ((e: { data: unknown }) => void)[] = [];
    const me = { post: (m: unknown) => listeners.forEach((l) => l({ data: m })) };
    this.members.push(me);
    return {
      postMessage: (m) => this.members.filter((x) => x !== me).forEach((x) => queueMicrotask(() => x.post(m))),
      addEventListener: (_t, l) => void listeners.push(l),
      close: () => undefined,
    };
  }
}

/** Web Locks stand-in: one holder at a time per name. */
function makeLocks(): Locks {
  const tails = new Map<string, Promise<unknown>>();
  return {
    request<T>(name: string, cb: () => Promise<T>): Promise<T> {
      const prev = tails.get(name) ?? Promise.resolve();
      const run = prev.then(cb, cb);
      tails.set(
        name,
        run.catch(() => undefined),
      );
      return run;
    },
  };
}

const HINT = 'todayz.hasSession.customer';
const PENDING = 'todayz.logoutPending.customer';

/** An unsigned JWT-shaped token whose payload carries `sub` (the session only decodes it). */
function jwt(sub: string, claims: Record<string, unknown> = {}): string {
  const b64 = (o: unknown) => btoa(JSON.stringify(o)).replace(/=+$/, '').replace(/\+/g, '-').replace(/\//g, '_');
  return `${b64({ alg: 'none' })}.${b64({ sub, ...claims })}.sig`;
}

const tick = (ms = 5) => new Promise<void>((r) => setTimeout(r, ms));
const soon = () => new Date(Date.now() + 15 * 60_000).toISOString();

const sessions: Session[] = [];
function make(server: ReturnType<typeof makeServer>, extra: Partial<Parameters<typeof createSession>[0]> = {}) {
  const s = createSession({
    app: 'customer',
    baseUrl: 'https://api.test',
    clientId: 'customer/1.0.0',
    fetch: server.fetch,
    locks: null,
    channel: null,
    graceWaitMs: 10,
    ...extra,
  });
  sessions.push(s);
  return s;
}

afterEach(() => {
  sessions.splice(0).forEach((s) => s.dispose());
  vi.useRealTimers();
  localStorage.clear();
  sessionStorage.clear();
});

describe('refresh', () => {
  it('starts from the cookie and keeps the token in memory only', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    const s = make(server);
    await s.start();
    expect(s.getState().status).toBe('signedIn');
    expect(s.getToken()).toBe('token-1');
    const dump = (st: Storage) =>
      Array.from({ length: st.length }, (_, i) => `${st.key(i)}=${st.getItem(st.key(i) ?? '')}`).join('\n');
    s.signIn({ accessToken: 'secret-access-token', expiresAt: new Date(Date.now() + 900_000).toISOString() });
    getDeviceId('customer');
    expect(localStorage.length).toBeGreaterThan(0);
    expect(dump(localStorage)).not.toContain('token');
    expect(dump(sessionStorage)).not.toContain('token');
    expect(document.cookie).not.toContain('token-1');
  });

  it('sends credentials and X-TZ-Client to refresh and never a bearer', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'stale', expiresAt: new Date(Date.now() + 1000).toISOString() });
    await s.refresh();
    const call = server.calls.find((c) => c.path === '/v1/auth/refresh');
    expect(call?.auth).toBeNull();
    expect(call?.client).toBe('customer/1.0.0');
    expect(call?.credentials).toBe('include');
  });

  it('runs one refresh at a time in a tab', async () => {
    const server = makeServer();
    server.state.refreshDelayMs = 20;
    const s = make(server);
    await Promise.all([s.refresh(), s.refresh(), s.refresh()]);
    expect(server.refreshCalls()).toBe(1);
  });

  it('runs one refresh at a time across tabs: the second tab takes the first one token', async () => {
    const server = makeServer();
    server.state.refreshDelayMs = 20;
    const bus = new Bus();
    const locks = makeLocks();
    const a = make(server, { locks, channel: bus.channel() });
    const b = make(server, { locks, channel: bus.channel() });
    const [ra, rb] = await Promise.all([a.refresh(), b.refresh()]);
    expect(ra && rb).toBe(true);
    expect(server.refreshCalls()).toBe(1);
    expect(a.getToken()).toBe('token-1');
    expect(b.getToken()).toBe('token-1');
  });

  it('refreshes before the token expires', async () => {
    vi.useFakeTimers();
    const server = makeServer();
    server.state.expiresInMs = 120_000;
    const s = make(server);
    s.signIn({ accessToken: 'first', expiresAt: new Date(Date.now() + 120_000).toISOString() });
    expect(server.refreshCalls()).toBe(0);
    await vi.advanceTimersByTimeAsync(59_000);
    expect(server.refreshCalls()).toBe(0);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(server.refreshCalls()).toBe(1);
    expect(s.getToken()).toBe('token-1');
  });

  it('a network failure does not sign out', async () => {
    const server = makeServer();
    const failing: typeof fetch = async () => {
      throw new TypeError('offline');
    };
    const s = make(server, { fetch: failing });
    s.signIn({ accessToken: 'a', expiresAt: new Date(Date.now() + 1000).toISOString() });
    expect(await s.refresh()).toBe(false);
    expect(s.getState()).toMatchObject({ status: 'signedIn', offline: true });
  });

  it('a 5xx does not sign out', async () => {
    const server = makeServer();
    server.state.refreshStatuses = [503];
    const s = make(server);
    s.signIn({ accessToken: 'a', expiresAt: new Date(Date.now() + 1000).toISOString() });
    expect(await s.refresh()).toBe(false);
    expect(s.getState().status).toBe('signedIn');
  });
});

describe('401 on refresh (D-105 grace)', () => {
  it('a second 401 signs out', async () => {
    const server = makeServer();
    server.state.refreshStatuses = [401, 401];
    const s = make(server);
    s.signIn({ accessToken: 'a', expiresAt: new Date(Date.now() + 1000).toISOString() });
    expect(await s.refresh()).toBe(false);
    expect(s.getState().status).toBe('signedOut');
    expect(s.getToken()).toBeNull();
    expect(server.refreshCalls()).toBe(2);
  });

  it('retries once and stays signed in when the retry succeeds', async () => {
    const server = makeServer();
    server.state.refreshStatuses = [401];
    const s = make(server);
    s.signIn({ accessToken: 'a', expiresAt: new Date(Date.now() + 1000).toISOString() });
    expect(await s.refresh()).toBe(true);
    expect(s.getState().status).toBe('signedIn');
    expect(server.refreshCalls()).toBe(2);
  });

  it('takes the other tab token when one arrives during the wait, with no second call', async () => {
    const server = makeServer();
    server.state.refreshStatuses = [401];
    const bus = new Bus();
    const a = make(server, { channel: bus.channel(), graceWaitMs: 30 });
    const other = bus.channel();
    a.signIn({ accessToken: 'a', expiresAt: new Date(Date.now() + 1000).toISOString() });
    const done = a.refresh();
    setTimeout(
      () =>
        other.postMessage({
          type: 'token',
          accessToken: 'from-other-tab',
          expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
          userId: null,
        }),
      5,
    );
    expect(await done).toBe(true);
    expect(a.getToken()).toBe('from-other-tab');
    expect(server.refreshCalls()).toBe(1);
  });
});

describe('API calls', () => {
  it('on a 401 refreshes once and retries the request once with the new bearer', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'revoked', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    const me = await request<{ id: string }>('/v1/me');
    expect(me.id).toBe('u1');
    const meCalls = server.calls.filter((c) => c.path === '/v1/me');
    expect(meCalls.map((c) => c.auth)).toEqual(['Bearer revoked', 'Bearer token-1']);
    expect(server.refreshCalls()).toBe(1);
  });

  it('does not loop: a request that is 401 after the refresh fails', async () => {
    const server = makeServer();
    const reject: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      if (path === '/v1/me') {
        const body = JSON.stringify({ type: 't', title: 't', status: 401, code: 'unauthorized' });
        return new Response(body, { status: 401, headers: { 'content-type': 'application/json' } });
      }
      return server.fetch(input, init);
    };
    const s2 = make(server, { fetch: reject });
    s2.signIn({ accessToken: 'revoked', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    await expect(request('/v1/me')).rejects.toMatchObject({ status: 401 });
    expect(server.refreshCalls()).toBe(1);
  });

  it('never sends the bearer to the sign-in routes', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'live', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    await request('/v1/auth/refresh', { method: 'POST', body: '{}' });
    await s.signOut();
    for (const c of server.calls.filter((c) => c.path.startsWith('/v1/auth/'))) expect(c.auth).toBeNull();
  });

  it('keeps the ETag of a GET for If-Match', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'x', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    server.state.validTokens.add('x');
    await request('/v1/me');
    expect(s.etag('/v1/me')).toBe('"v1"');
  });
});

describe('sign out', () => {
  it('revokes, forgets the token and tells the other tabs', async () => {
    const server = makeServer();
    const bus = new Bus();
    const a = make(server, { channel: bus.channel() });
    const b = make(server, { channel: bus.channel() });
    a.signIn({ accessToken: 'x', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    await Promise.resolve();
    expect(b.getState().status).toBe('signedIn');
    await a.signOut();
    await Promise.resolve();
    expect(a.getToken()).toBeNull();
    expect(b.getState().status).toBe('signedOut');
    expect(server.calls.some((c) => c.path === '/v1/auth/logout')).toBe(true);
  });

  it('signs out locally even when the network is down', async () => {
    const server = makeServer();
    const failing: typeof fetch = async () => {
      throw new TypeError('offline');
    };
    const s = make(server, { fetch: failing });
    s.signIn({ accessToken: 'x', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() });
    await s.signOut();
    expect(s.getState().status).toBe('signedOut');
  });
});

describe('credentials on every auth call (review 1)', () => {
  it('sends credentials: include on each /v1/auth/* route and on nothing else', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'live', expiresAt: soon() });
    server.state.validTokens.add('live');
    const routes = [
      '/v1/auth/otp/send',
      '/v1/auth/otp/verify',
      '/v1/auth/refresh',
      '/v1/auth/logout',
      '/v1/auth/logout-all',
      '/v1/auth/step-up/send',
      '/v1/auth/step-up/verify',
    ];
    for (const path of routes) await request(path, { method: 'POST', body: '{}' }).catch(() => undefined);
    await request('/v1/me');
    for (const path of routes) {
      const c = server.calls.find((x) => x.path === path);
      expect(c, path).toBeDefined();
      expect(c?.credentials, path).toBe('include');
      // The admin step-up routes are bearer-authenticated (the person is already signed in, D-264).
      if (!path.includes('/step-up/')) expect(c?.auth, path).toBeNull();
    }
    expect(server.calls.find((x) => x.path === '/v1/me')?.credentials).toBeUndefined();
  });

  it('logout-all is a cookie route: no bearer, and a 401 is not retried', async () => {
    const server = makeServer();
    const reject: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      if (path === '/v1/auth/logout-all') {
        return new Response(JSON.stringify({ type: 't', title: 't', status: 401, code: 'unauthorized' }), {
          status: 401,
          headers: { 'content-type': 'application/problem+json' },
        });
      }
      return server.fetch(input, init);
    };
    const s = make(server, { fetch: reject });
    s.signIn({ accessToken: 'live', expiresAt: soon() });
    await request('/v1/auth/logout-all', { method: 'POST', body: '{"app":"customer"}' }).catch(() => undefined);
    expect(server.refreshCalls()).toBe(0);
  });
});

describe('401 decided by problem code', () => {
  const answer = (status: number, code?: string) =>
    code === undefined
      ? new Response(null, { status })
      : new Response(JSON.stringify({ type: 't', title: 't', status, code }), {
          status,
          headers: { 'content-type': 'application/problem+json' },
        });

  it('hands an otp_expired 401 to the caller: no refresh, no sign-out', async () => {
    const server = makeServer();
    const f: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      return path === '/v1/me' ? answer(401, 'otp_expired') : server.fetch(input, init);
    };
    const s = make(server, { fetch: f });
    s.signIn({ accessToken: 'live', expiresAt: soon() });
    await expect(request('/v1/me', { method: 'DELETE' })).rejects.toMatchObject({ status: 401, code: 'otp_expired' });
    expect(server.refreshCalls()).toBe(0);
    expect(s.getState().status).toBe('signedIn');
  });

  it('refreshes and retries once on a plain unauthorized, and on a 401 with no body', async () => {
    for (const code of ['unauthorized', undefined]) {
      const server = makeServer();
      const f: typeof fetch = async (input, init) => {
        const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
        if (path === '/v1/me' && new Headers(init?.headers).get('authorization') === 'Bearer stale') return answer(401, code);
        return server.fetch(input, init);
      };
      const s = make(server, { fetch: f });
      s.signIn({ accessToken: 'stale', expiresAt: soon() });
      const me = await request<{ id: string }>('/v1/me');
      expect(me.id).toBe('u1');
      expect(server.refreshCalls()).toBe(1);
      s.dispose();
    }
  });

  function logoutAllServer(statuses: number[]) {
    const server = makeServer();
    let n = 0;
    const f: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      if (path === '/v1/auth/logout-all') {
        n += 1;
        const status = statuses.shift() ?? 204;
        return status === 204 ? new Response(null, { status }) : answer(status, 'unauthorized');
      }
      return server.fetch(input, init);
    };
    return { server, f, attempts: () => n };
  }

  it('logout-all: a 401 refreshes once, retries once, then signs out everywhere', async () => {
    const { server, f, attempts } = logoutAllServer([401]);
    const s = make(server, { fetch: f });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    await s.signOutEverywhere();
    expect(attempts()).toBe(2);
    expect(server.refreshCalls()).toBe(1);
    expect(s.getState().status).toBe('signedOut');
  });

  it('logout-all sends credentials and X-TZ-Client like refresh, and never a bearer', async () => {
    const server = makeServer();
    let seen: { client: string | null; auth: string | null; credentials: RequestCredentials | undefined } | undefined;
    const f: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      if (path === '/v1/auth/logout-all') {
        const h = new Headers(init?.headers);
        seen = { client: h.get('X-TZ-Client'), auth: h.get('Authorization'), credentials: init?.credentials };
        return new Response(null, { status: 204 });
      }
      return server.fetch(input, init);
    };
    const s = make(server, { fetch: f });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    await s.signOutEverywhere();
    expect(seen).toEqual({ client: 'customer/1.0.0', auth: null, credentials: 'include' });
  });

  it('logout-all: a second 401 throws and does not sign this tab out', async () => {
    const { server, f, attempts } = logoutAllServer([401, 401]);
    const s = make(server, { fetch: f });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    await expect(s.signOutEverywhere()).rejects.toMatchObject({ status: 401 });
    expect(attempts()).toBe(2);
    expect(s.getState().status).toBe('signedIn');
  });
});

describe('cold start (review 2, 7)', () => {
  const offline = (server: ReturnType<typeof makeServer>, down: { v: boolean }): typeof fetch =>
    async (input, init) => {
      if (down.v) throw new TypeError('offline');
      return server.fetch(input, init);
    };

  it('stays unknown when the network is down, then signs in on the online event', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    const down = { v: true };
    const s = make(server, { fetch: offline(server, down) });
    await s.start();
    expect(s.getState()).toMatchObject({ status: 'unknown', offline: true });
    down.v = false;
    window.dispatchEvent(new Event('online'));
    await vi.waitFor(() => expect(s.getState().status).toBe('signedIn'));
    expect(s.getToken()).toBe('token-1');
  });

  it('stays unknown on a 5xx and recovers when the tab becomes visible', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    server.state.refreshStatuses = [503];
    const s = make(server);
    await s.start();
    expect(s.getState()).toMatchObject({ status: 'unknown', offline: true });
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.waitFor(() => expect(s.getState().status).toBe('signedIn'));
  });

  it('retries by itself with a growing pause', async () => {
    vi.useFakeTimers();
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    server.state.refreshStatuses = [503, 503];
    const s = make(server);
    await s.start();
    expect(s.getState().status).toBe('unknown');
    await vi.advanceTimersByTimeAsync(5_000);
    expect(server.refreshCalls()).toBe(2);
    expect(s.getState().status).toBe('unknown');
    await vi.advanceTimersByTimeAsync(10_000);
    expect(server.refreshCalls()).toBe(3);
    expect(s.getState().status).toBe('signedIn');
  });

  it('only a final 401 is signed out, with no second call and no grace wait', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    server.state.refreshStatuses = [401];
    const s = make(server, { graceWaitMs: 60_000, locks: makeLocks() });
    await s.start();
    expect(s.getState().status).toBe('signedOut');
    expect(server.refreshCalls()).toBe(1);
    expect(localStorage.getItem(HINT)).toBeNull();
  });

  it('without Web Locks a 401 for a tab that never held a token leaves the shared hint alone (L5)', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    server.state.refreshStatuses = [401];
    const s = make(server);
    await s.start();
    expect(s.getState().status).toBe('signedOut');
    expect(localStorage.getItem(HINT)).toBe('1');
  });

  it('makes no request for a visitor who never signed in here', async () => {
    const server = makeServer();
    const s = make(server);
    await s.start();
    expect(s.getState().status).toBe('signedOut');
    expect(server.calls).toHaveLength(0);
  });

  it('sets the hint at sign-in and clears it at sign-out', async () => {
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'x', expiresAt: soon() });
    expect(localStorage.getItem(HINT)).toBe('1');
    await s.signOut();
    expect(localStorage.getItem(HINT)).toBeNull();
  });

  it('waits for the other tab only when this tab held a token', async () => {
    const server = makeServer();
    server.state.refreshStatuses = [401];
    const s = make(server, { graceWaitMs: 40 });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    const t0 = Date.now();
    await s.refresh();
    expect(Date.now() - t0).toBeGreaterThanOrEqual(35);
    expect(server.refreshCalls()).toBe(2);
  });
});

describe('sign-out races (review 3)', () => {
  it('a refresh that resolves after sign-out does not sign the person back in', async () => {
    const server = makeServer();
    server.state.refreshDelayMs = 40;
    const s = make(server);
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    const pending = s.refresh();
    await s.signOut();
    await pending;
    expect(s.getState().status).toBe('signedOut');
    expect(s.getToken()).toBeNull();
    expect(localStorage.getItem(HINT)).toBeNull();
  });

  it('ignores a token message older than the latest sign-out', async () => {
    const server = makeServer();
    const bus = new Bus();
    const a = make(server, { channel: bus.channel() });
    const other = bus.channel();
    other.postMessage({ type: 'signout', at: 2000 });
    await tick();
    expect(a.getState().status).toBe('signedOut');
    other.postMessage({ type: 'token', accessToken: 'late', expiresAt: soon(), userId: null, at: 1000 });
    await tick();
    expect(a.getState().status).toBe('signedOut');
    expect(a.getToken()).toBeNull();
    other.postMessage({ type: 'token', accessToken: 'newer', expiresAt: soon(), userId: null, at: 3000 });
    await tick();
    expect(a.getToken()).toBe('newer');
  });

  it('ignores a sign-out message older than the token it already holds', async () => {
    const server = makeServer();
    const bus = new Bus();
    const a = make(server, { channel: bus.channel() });
    const other = bus.channel();
    other.postMessage({ type: 'token', accessToken: 'fresh', expiresAt: soon(), userId: null, at: 5000 });
    await tick();
    other.postMessage({ type: 'signout', at: 4000 });
    await tick();
    expect(a.getToken()).toBe('fresh');
    expect(a.getState().status).toBe('signedIn');
  });
});

describe('offline sign-out (review 4)', () => {
  const down: typeof fetch = async () => {
    throw new TypeError('offline');
  };

  it('remembers a sign-out that could not reach the server', async () => {
    const server = makeServer();
    const s = make(server, { fetch: down });
    s.signIn({ accessToken: 'x', expiresAt: soon() });
    await s.signOut();
    expect(localStorage.getItem(PENDING)).toBe('1');
    expect(s.getState().status).toBe('signedOut');
  });

  it('retries the logout on the next load before any refresh, and does not sign back in', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    localStorage.setItem(PENDING, '1');
    const s = make(server);
    await s.start();
    expect(server.calls.map((c) => c.path)).toEqual(['/v1/auth/logout']);
    expect(s.getState().status).toBe('signedOut');
    expect(localStorage.getItem(PENDING)).toBeNull();
    expect(localStorage.getItem(HINT)).toBeNull();
  });

  it('keeps the hint when the retry fails too, and still does not refresh', async () => {
    const server = makeServer();
    localStorage.setItem(HINT, '1');
    localStorage.setItem(PENDING, '1');
    const s = make(server, { fetch: down });
    await s.start();
    expect(s.getState().status).toBe('signedOut');
    expect(localStorage.getItem(PENDING)).toBe('1');
    expect(server.refreshCalls()).toBe(0);
  });

  it('a new sign-in drops the pending hint', () => {
    const server = makeServer();
    localStorage.setItem(PENDING, '1');
    const s = make(server);
    s.signIn({ accessToken: 'x', expiresAt: soon() });
    expect(localStorage.getItem(PENDING)).toBeNull();
  });
});

describe('hung refresh and locks (review 5)', () => {
  it('a refresh that never answers times out and counts as failed', async () => {
    const server = makeServer();
    const hang: typeof fetch = (_input, init) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
      });
    const s = make(server, { fetch: hang, refreshTimeoutMs: 20 });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    expect(await s.refresh()).toBe(false);
    expect(s.getState()).toMatchObject({ status: 'signedIn', offline: true });
  });

  it('falls back to the in-tab path when locks.request throws', async () => {
    const server = makeServer();
    const locks: Locks = {
      request() {
        throw new DOMException('insecure', 'SecurityError');
      },
    };
    const s = make(server, { locks });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    expect(await s.refresh()).toBe(true);
    expect(s.getToken()).toBe('token-1');
  });

  it('falls back when locks.request rejects before running the callback', async () => {
    const server = makeServer();
    const locks: Locks = { request: () => Promise.reject(new DOMException('denied', 'SecurityError')) };
    const s = make(server, { locks });
    s.signIn({ accessToken: 'a', expiresAt: soon() });
    expect(await s.refresh()).toBe(true);
  });

  it('start() and the timer never leave an unhandled rejection', async () => {
    const seen: unknown[] = [];
    const onRejection = (e: unknown) => void seen.push(e);
    process.on('unhandledRejection', onRejection);
    try {
      const server = makeServer();
      localStorage.setItem(HINT, '1');
      const boom: typeof fetch = async () => {
        throw new Error('not an ApiError');
      };
      const locks: Locks = {
        request() {
          throw new Error('locks broke');
        },
      };
      const s = make(server, { fetch: boom, locks });
      await s.start();
      await tick(30);
      expect(s.getState().status).toBe('unknown');
    } finally {
      process.off('unhandledRejection', onRejection);
    }
    expect(seen).toEqual([]);
  });
});

describe('refresh timing (review 6)', () => {
  it('never waits less than 30 s, however short the token lives', async () => {
    vi.useFakeTimers();
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'short', expiresAt: new Date(Date.now() + 10_000).toISOString() });
    await vi.advanceTimersByTimeAsync(29_000);
    expect(server.refreshCalls()).toBe(0);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(server.refreshCalls()).toBe(1);
  });

  it('treats an expiry that is not a date as a short fallback life, not NaN', async () => {
    vi.useFakeTimers();
    const server = makeServer();
    const s = make(server);
    s.signIn({ accessToken: 'odd', expiresAt: 'not a date' });
    await vi.advanceTimersByTimeAsync(29_000);
    expect(server.refreshCalls()).toBe(0);
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    expect(server.refreshCalls()).toBe(1);
  });

  it('counts the token life from its own exp - iat, not from either clock or a Date header', async () => {
    vi.useFakeTimers();
    const server = makeServer();
    // exp and iat are stamped by a server whose clock is hours away from this device's.
    const iat = Math.floor(Date.now() / 1000) + 3 * 3600;
    const token = jwt('u1', { iat, exp: iat + 120 });
    const odd: typeof fetch = async (input, init) => {
      const path = new URL(typeof input === 'string' ? input : (input as Request).url, 'http://x').pathname;
      if (path !== '/v1/auth/refresh') return server.fetch(input, init);
      server.calls.push({ method: 'POST', path, auth: null, client: null, credentials: 'include' });
      return new Response(JSON.stringify({ accessToken: token, tokenType: 'Bearer', expiresAt: new Date(Date.now() + 3 * 3600_000 + 120_000).toISOString() }), {
        status: 200,
        headers: { 'content-type': 'application/json', date: new Date(Date.now() - 86_400_000).toUTCString() },
      });
    };
    const s = make(server, { fetch: odd });
    s.signIn({ accessToken: 'stale', expiresAt: new Date(Date.now() + 1000).toISOString() });
    await s.refresh();
    expect(server.refreshCalls()).toBe(1);
    await vi.advanceTimersByTimeAsync(58_000);
    expect(server.refreshCalls()).toBe(1);
    await vi.advanceTimersByTimeAsync(4_000);
    expect(server.refreshCalls()).toBe(2);
  });
});

describe('user switch across tabs (review 8)', () => {
  it('tells the app when another tab signs in as a different user', async () => {
    const server = makeServer();
    const bus = new Bus();
    const a = make(server, { channel: bus.channel() });
    const b = make(server, { channel: bus.channel() });
    const changes: [string, string][] = [];
    a.onUserChange((prev, next) => changes.push([prev, next]));
    a.signIn({ accessToken: jwt('user-1'), expiresAt: soon() });
    await tick();
    expect(a.getState().userId).toBe('user-1');
    b.signIn({ accessToken: jwt('user-2'), expiresAt: soon() });
    await tick();
    expect(changes).toEqual([['user-1', 'user-2']]);
    expect(a.getState().userId).toBe('user-2');
  });

  it('is quiet when the same user gets a new token', async () => {
    const server = makeServer();
    const bus = new Bus();
    const a = make(server, { channel: bus.channel() });
    const b = make(server, { channel: bus.channel() });
    const listener = vi.fn();
    a.onUserChange(listener);
    a.signIn({ accessToken: jwt('user-1'), expiresAt: soon() });
    b.signIn({ accessToken: jwt('user-1'), expiresAt: soon() });
    await tick();
    expect(listener).not.toHaveBeenCalled();
  });

  it('fires for a different user signing in on this tab, and drops the old ETags', async () => {
    const server = makeServer();
    const s = make(server);
    const listener = vi.fn();
    s.onUserChange(listener);
    s.signIn({ accessToken: jwt('user-1'), expiresAt: soon() });
    server.state.validTokens.add(jwt('user-1'));
    await request('/v1/me');
    expect(s.etag('/v1/me')).toBe('"v1"');
    s.signIn({ accessToken: jwt('user-2'), expiresAt: soon() });
    expect(listener).toHaveBeenCalledWith('user-1', 'user-2');
    expect(s.etag('/v1/me')).toBeUndefined();
  });
});

describe('second review round', () => {
  const hang: typeof fetch = (_input, init) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    });

  describe('M1 sign-out order', () => {
    it('forgets the token before the logout call returns, and a refresh asked for meanwhile does nothing', async () => {
      const server = makeServer();
      server.state.logoutDelayMs = 40;
      const s = make(server);
      s.signIn({ accessToken: 'a', expiresAt: soon() });
      const out = s.signOut();
      expect(s.getToken()).toBeNull();
      expect(s.getState().status).toBe('signedOut');
      expect(localStorage.getItem(PENDING)).toBe('1');
      expect(await s.refresh()).toBe(false);
      await out;
      expect(server.refreshCalls()).toBe(0);
      expect(s.getState().status).toBe('signedOut');
      expect(localStorage.getItem(PENDING)).toBeNull();
    });

    it('drops a refresh that was queued on the lock behind a sign-out', async () => {
      const server = makeServer();
      server.state.refreshDelayMs = 40;
      const locks = makeLocks();
      const a = make(server, { locks });
      const b = make(server, { locks });
      b.signIn({ accessToken: 'b-old', expiresAt: soon() });
      const first = a.refresh();
      const queued = b.refresh();
      await b.signOut();
      await Promise.all([first, queued]);
      expect(server.refreshCalls()).toBe(1);
      expect(b.getToken()).toBeNull();
      expect(b.getState().status).toBe('signedOut');
    });
  });

  describe('M2 hint without storage', () => {
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => undefined,
      removeItem: () => undefined,
    };
    it('tries the cookie when the store throws', async () => {
      const server = makeServer();
      const s = make(server, { storage: broken });
      await s.start();
      expect(server.refreshCalls()).toBe(1);
      expect(s.getState().status).toBe('signedIn');
    });
    it('tries the cookie when there is no store', async () => {
      const server = makeServer();
      const s = make(server, { storage: null });
      await s.start();
      expect(server.refreshCalls()).toBe(1);
      expect(s.getState().status).toBe('signedIn');
    });
  });

  describe('M4 user change after a sign-out', () => {
    it('fires when a different user signs in after a sign-out', async () => {
      const server = makeServer();
      const s = make(server);
      const listener = vi.fn();
      s.onUserChange(listener);
      s.signIn({ accessToken: jwt('u1'), expiresAt: soon() });
      await s.signOut();
      s.signIn({ accessToken: jwt('u2'), expiresAt: soon() });
      expect(listener).toHaveBeenCalledWith('u1', 'u2');
    });
    it('fires after a final 401 too, and not for the same user', async () => {
      const server = makeServer();
      server.state.refreshStatuses = [401, 401];
      const s = make(server);
      const listener = vi.fn();
      s.onUserChange(listener);
      s.signIn({ accessToken: jwt('u1'), expiresAt: soon() });
      await s.refresh();
      expect(s.getState().status).toBe('signedOut');
      s.signIn({ accessToken: jwt('u1'), expiresAt: soon() });
      expect(listener).not.toHaveBeenCalled();
      await s.signOut();
      s.signIn({ accessToken: jwt('u3'), expiresAt: soon() });
      expect(listener).toHaveBeenCalledWith('u1', 'u3');
    });
  });

  describe('M6 logout timeout', () => {
    it('gives up on a logout that never answers and keeps it pending', async () => {
      const server = makeServer();
      const s = make(server, { fetch: hang, logoutTimeoutMs: 20 });
      s.signIn({ accessToken: 'a', expiresAt: soon() });
      await s.signOut();
      expect(s.getState().status).toBe('signedOut');
      expect(localStorage.getItem(PENDING)).toBe('1');
    });
    it('the retry on the next load times out as well', async () => {
      const server = makeServer();
      localStorage.setItem(PENDING, '1');
      const s = make(server, { fetch: hang, logoutTimeoutMs: 20 });
      await s.start();
      expect(s.getState().status).toBe('signedOut');
      expect(localStorage.getItem(PENDING)).toBe('1');
    });
  });

  describe('L2 stamps', () => {
    it('a tie goes to the sign-out', async () => {
      const server = makeServer();
      const bus = new Bus();
      const a = make(server, { channel: bus.channel() });
      const other = bus.channel();
      other.postMessage({ type: 'signout', at: 2000 });
      await tick();
      other.postMessage({ type: 'token', accessToken: 'tie', expiresAt: soon(), userId: null, at: 2000 });
      await tick();
      expect(a.getToken()).toBeNull();
      other.postMessage({ type: 'token', accessToken: 'later', expiresAt: soon(), userId: null, at: 3000 });
      await tick();
      other.postMessage({ type: 'signout', at: 3000 });
      await tick();
      expect(a.getToken()).toBeNull();
      expect(a.getState().status).toBe('signedOut');
    });
    it('a local stamp is never behind one it has seen', async () => {
      const server = makeServer();
      const bus = new Bus();
      const a = make(server, { channel: bus.channel() });
      const other = bus.channel();
      const seen: { type: string; at: number }[] = [];
      other.addEventListener('message', (e) => seen.push(e.data as { type: string; at: number }));
      const future = Date.now() + 10_000_000;
      other.postMessage({ type: 'token', accessToken: 'far', expiresAt: soon(), userId: null, at: future });
      await tick();
      await a.signOut();
      await tick();
      const out = seen.find((m) => m.type === 'signout');
      expect(out?.at).toBeGreaterThan(future);
    });
  });

  describe('L3 signed-in retries', () => {
    it('retries a failed refresh with a growing pause, then stops after a few tries', async () => {
      vi.useFakeTimers();
      const server = makeServer();
      server.state.refreshStatuses = Array.from({ length: 30 }, () => 503);
      const s = make(server);
      s.signIn({ accessToken: 'a', expiresAt: soon() });
      await s.refresh();
      expect(server.refreshCalls()).toBe(1);
      await vi.advanceTimersByTimeAsync(5_000);
      expect(server.refreshCalls()).toBe(2);
      await vi.advanceTimersByTimeAsync(10_000);
      expect(server.refreshCalls()).toBe(3);
      await vi.advanceTimersByTimeAsync(10 * 60_000);
      expect(server.refreshCalls()).toBe(6);
      expect(s.getState().status).toBe('signedIn');
    });
    it('a retry that works clears the offline flag', async () => {
      vi.useFakeTimers();
      const server = makeServer();
      server.state.refreshStatuses = [503];
      const s = make(server);
      s.signIn({ accessToken: 'a', expiresAt: soon() });
      await s.refresh();
      expect(s.getState().offline).toBe(true);
      await vi.advanceTimersByTimeAsync(5_000);
      expect(s.getState()).toMatchObject({ status: 'signedIn', offline: false });
    });
  });

  describe('L4 logout flush', () => {
    it('sends one logout however many wake-ups arrive', async () => {
      const server = makeServer();
      server.state.logoutDelayMs = 30;
      localStorage.setItem(PENDING, '1');
      const s = make(server);
      const started = s.start();
      window.dispatchEvent(new Event('online'));
      document.dispatchEvent(new Event('visibilitychange'));
      await started;
      expect(server.calls.filter((c) => c.path === '/v1/auth/logout')).toHaveLength(1);
    });
    it('a sign-in while the flush is on its way keeps the new session', async () => {
      const server = makeServer();
      server.state.logoutDelayMs = 30;
      localStorage.setItem(PENDING, '1');
      const s = make(server);
      const started = s.start();
      s.signIn({ accessToken: 'new', expiresAt: soon() });
      await started;
      expect(s.getState().status).toBe('signedIn');
      expect(localStorage.getItem(PENDING)).toBeNull();
    });
  });
});

describe('device id', () => {
  beforeEach(() => localStorage.clear());
  it('is a ULID kept per app', () => {
    const a = getDeviceId('customer');
    expect(a).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    expect(getDeviceId('customer')).toBe(a);
    expect(getDeviceId('business')).not.toBe(a);
  });
  it('survives a storage that throws', () => {
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(getDeviceId('admin', broken)).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
  });
});
