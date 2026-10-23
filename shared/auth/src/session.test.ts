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
    if (path === '/v1/auth/logout') return new Response(null, { status: 204 });
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
