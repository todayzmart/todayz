import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError, configure, fallbackProblem, request } from '../src/http';

type Call = { url: string; init: RequestInit };

/** A fetch stub that records the call and answers with `res`. */
function stub(res: Response) {
  const calls: Call[] = [];
  const fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    calls.push({ url: String(url), init: init ?? {} });
    return res;
  });
  return { calls, fetch: fetch as unknown as typeof globalThis.fetch };
}

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });
}

function problem(code: string, status: number, extra: Record<string, unknown> = {}): Response {
  return new Response(
    JSON.stringify({ type: `https://todayz.in/errors/${code}`, title: code, status, code, ...extra }),
    { status, headers: { 'content-type': 'application/problem+json' } },
  );
}

/** The n-th recorded call, failing the test when it was not made. */
function call(s: { calls: Call[] }, n = 0): Call {
  const c = s.calls[n];
  if (!c) throw new Error(`no call ${n}`);
  return c;
}

function sentHeaders(s: { calls: Call[] }): Headers {
  return new Headers(call(s).init.headers);
}

afterEach(() => configure({}));

describe('request credentials and bearer on auth routes', () => {
  const cookieRoutes = ['otp/send', 'otp/verify', 'refresh', 'logout', 'logout-all'];

  it('sends the cookie to every /v1/auth/ route, and to nothing else by default', async () => {
    for (const r of [...cookieRoutes, 'totp/verify', 'totp/recover', 'totp/enrol']) {
      const s = stub(json({}));
      configure({ fetch: s.fetch });
      await request(`/v1/auth/${r}`, { method: 'POST' });
      expect(call(s).init.credentials, r).toBe('include');
    }
    const s = stub(json({}));
    configure({ fetch: s.fetch });
    await request('/v1/me');
    expect(call(s).init.credentials).toBeUndefined();
  });

  it('never sends the bearer to the cookie routes, but does to the TOTP routes', async () => {
    for (const r of cookieRoutes) {
      const s = stub(json({}));
      configure({ fetch: s.fetch, token: () => 'tok' });
      await request(`/v1/auth/${r}`, { method: 'POST' });
      expect(sentHeaders(s).get('authorization'), r).toBeNull();
    }
    const s = stub(json({}));
    configure({ fetch: s.fetch, token: () => 'tok' });
    await request('/v1/auth/totp/verify', { method: 'POST' });
    expect(sentHeaders(s).get('authorization')).toBe('Bearer tok');
  });
});

describe('request', () => {
  it('resolves the decoded JSON body of a 2xx answer', async () => {
    const s = stub(json({ status: 'ok' }));
    configure({ fetch: s.fetch });
    await expect(request<{ status: string }>('/healthz')).resolves.toEqual({ status: 'ok' });
    expect(s.calls[0]?.url).toBe('/healthz');
    expect(sentHeaders(s).get('accept')).toBe('application/json, application/problem+json');
  });

  it('resolves undefined for 204 and for an empty 200 body', async () => {
    const s = stub(new Response(null, { status: 204 }));
    configure({ fetch: s.fetch });
    await expect(request('/v1/me/x')).resolves.toBeUndefined();
    configure({ fetch: stub(new Response('', { status: 200 })).fetch });
    await expect(request('/v1/me/x')).resolves.toBeUndefined();
  });

  it('prefixes baseUrl and passes method and body through', async () => {
    const s = stub(json({ now: 'x' }));
    configure({ fetch: s.fetch, baseUrl: 'https://api.todayz.localhost' });
    await request('/v1/test/clock', {
      method: 'POST',
      body: JSON.stringify({ advance: '31m' }),
      headers: { 'Content-Type': 'application/json' },
    });
    expect(s.calls[0]?.url).toBe('https://api.todayz.localhost/v1/test/clock');
    expect(s.calls[0]?.init.method).toBe('POST');
    expect(s.calls[0]?.init.body).toBe('{"advance":"31m"}');
    expect(sentHeaders(s).get('content-type')).toBe('application/json');
  });

  it('sends Authorization, Accept-Language, Idempotency-Key and If-Match when given', async () => {
    const s = stub(json({}));
    configure({ fetch: s.fetch, token: async () => 'tok', language: () => 'hi-IN' });
    await request('/v1/me', { idempotencyKey: '01J0000000000000000000000X', ifMatch: '"7"' });
    const h = sentHeaders(s);
    expect(h.get('authorization')).toBe('Bearer tok');
    expect(h.get('accept-language')).toBe('hi-IN');
    expect(h.get('idempotency-key')).toBe('01J0000000000000000000000X');
    expect(h.get('if-match')).toBe('"7"');
    // The client-only options never reach fetch's init.
    expect('idempotencyKey' in call(s).init).toBe(false);
    expect('ifMatch' in call(s).init).toBe(false);
  });

  it('never sends the bearer to the sign-in routes', async () => {
    const calls: Call[] = [];
    const s = { calls };
    configure({
      token: () => 'tok',
      fetch: (async (url: string | URL | Request, init?: RequestInit) => {
        calls.push({ url: String(url), init: init ?? {} });
        return json({});
      }) as unknown as typeof globalThis.fetch,
    });
    for (const p of ['/v1/auth/otp/send', '/v1/auth/otp/verify', '/v1/auth/refresh', '/v1/auth/logout?x=1']) {
      await request(p, { method: 'POST' });
    }
    await request('/v1/me');
    expect(s.calls.slice(0, 4).every((c) => !new Headers(c.init.headers).has('authorization'))).toBe(true);
    expect(new Headers(call(s, 4).init.headers).get('authorization')).toBe('Bearer tok');
  });

  it('sends no Authorization or Accept-Language when signed out', async () => {
    const s = stub(json({}));
    configure({ fetch: s.fetch, token: () => null, language: () => undefined });
    await request('/v1/discover/search');
    const h = sentHeaders(s);
    expect(h.has('authorization')).toBe(false);
    expect(h.has('accept-language')).toBe(false);
    expect(h.has('idempotency-key')).toBe(false);
  });

  it('throws ApiError carrying the problem+json body', async () => {
    configure({
      fetch: stub(
        problem('validation_failed', 422, {
          detail: 'quantity must be at least 1',
          fields: { 'body.items[0].quantity': 'min' },
          requestId: 'req_1',
        }),
      ).fetch,
    });
    const err = await request('/v1/me/orders', { method: 'POST' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    const e = err as ApiError;
    expect(e.status).toBe(422);
    expect(e.code).toBe('validation_failed');
    expect(e.fields).toEqual({ 'body.items[0].quantity': 'min' });
    expect(e.problem.requestId).toBe('req_1');
    expect(e.message).toBe('validation_failed (422): quantity must be at least 1');
  });

  it('serialises to code, status and requestId only (D-100, security L-6)', async () => {
    configure({
      fetch: stub(
        problem('validation_failed', 422, {
          detail: 'phone +91 98765 43210 is not valid',
          fields: { 'body.phone': 'invalid format' },
          extensions: { quote: { totalPaise: 1 } },
          requestId: 'req_2',
        }),
      ).fetch,
    });
    const e = (await request('/v1/auth/otp', { method: 'POST' }).catch((x: unknown) => x)) as ApiError;
    expect(e.toJSON()).toEqual({ code: 'validation_failed', status: 422, requestId: 'req_2' });
    const json = JSON.stringify({ err: e });
    expect(json).toBe('{"err":{"code":"validation_failed","status":422,"requestId":"req_2"}}');
    expect(json).not.toContain('98765');
    expect(new ApiError(fallbackProblem(503), new Headers()).toJSON()).toEqual({ code: 'internal', status: 503 });
  });

  it('fills requestId from X-Request-Id when the body has none', async () => {
    const res = new Response(JSON.stringify({ type: 't', title: 'Not found', status: 404, code: 'not_found' }), {
      status: 404,
      headers: { 'content-type': 'application/problem+json', 'x-request-id': 'req_9' },
    });
    configure({ fetch: stub(res).fetch });
    const e = (await request('/v1/me/orders/x').catch((x: unknown) => x)) as ApiError;
    expect(e.code).toBe('not_found');
    expect(e.problem.requestId).toBe('req_9');
    expect(e.fields).toEqual({});
  });

  it('falls back to the registered internal code for a 5xx without problem JSON', async () => {
    const res = new Response('<html>502</html>', {
      status: 502,
      headers: { 'content-type': 'text/html', 'x-request-id': 'req_5' },
    });
    configure({ fetch: stub(res).fetch });
    const e = (await request('/v1/me').catch((x: unknown) => x)) as ApiError;
    expect(e.code).toBe('internal');
    expect(e.status).toBe(502);
    expect(e.problem.type).toBe('https://todayz.in/errors/internal');
    expect(e.problem.requestId).toBe('req_5');
  });

  it('falls back to bad_request for a 4xx without problem JSON', async () => {
    configure({ fetch: stub(new Response('nope', { status: 400 })).fetch });
    const e = (await request('/v1/me').catch((x: unknown) => x)) as ApiError;
    expect(e.code).toBe('bad_request');
    expect(e.problem.requestId).toBeUndefined();
  });

  it('treats JSON that is not a problem as a fallback too', async () => {
    configure({ fetch: stub(json({ error: 'old shape' }, 500)).fetch });
    const e = (await request('/v1/me').catch((x: unknown) => x)) as ApiError;
    expect(e.code).toBe('internal');
  });

  it('keeps an Accept the caller set and resolves a text/* body as text (a CSV export)', async () => {
    const s = stub(new Response('a,b\n1,2\n', { status: 200, headers: { 'content-type': 'text/csv; charset=utf-8' } }));
    configure({ fetch: s.fetch });
    await expect(
      request('/v1/biz/businesses/01J0000000000000000000000X/export', { headers: { Accept: 'text/csv' } }),
    ).resolves.toBe('a,b\n1,2\n');
    expect(sentHeaders(s).get('accept')).toBe('text/csv');
    const h = stub(json({}));
    configure({ fetch: h.fetch });
    await request('/v1/me', { headers: new Headers({ accept: 'text/event-stream' }) });
    expect(sentHeaders(h).get('accept')).toBe('text/event-stream');
  });

  it('resolves by Content-Type: JSON parsed, +json parsed, anything else a Blob (D-100 round 2, Low 10)', async () => {
    configure({ fetch: stub(json({ a: 1 })).fetch });
    await expect(request('/v1/me')).resolves.toEqual({ a: 1 });
    configure({ fetch: stub(json({ b: 2 }, 200, { 'content-type': 'application/vnd.todayz+json' })).fetch });
    await expect(request('/v1/me')).resolves.toEqual({ b: 2 });
    configure({ fetch: stub(new Response(new Uint8Array([0x25, 0x50, 0x44, 0x46]), { status: 200, headers: { 'content-type': 'application/pdf' } })).fetch });
    const blob = await request<Blob>('/v1/me/orders/01J0000000000000000000000X/receipt');
    expect(blob).toBeInstanceOf(Blob);
    expect(blob.type).toBe('application/pdf');
    expect(blob.size).toBe(4);
    configure({ fetch: stub(new Response('', { status: 200, headers: { 'content-type': 'application/json' } })).fetch });
    await expect(request('/v1/me')).resolves.toBeUndefined();
  });

  it('refuses a path with a backslash or a control character before fetching (D-100 round 2, Low 11)', async () => {
    const s = stub(json({}));
    configure({ fetch: s.fetch });
    for (const p of ['/v1/me\\orders', '/v1/me/orders?x=a\\b', '/v1/me\torders', '/v1/me\r\n', '/v1/me\n', '/v1/me?q=\r']) {
      await expect(request(p), JSON.stringify(p)).rejects.toThrow(/backslash or a control character/);
    }
    expect(s.calls).toHaveLength(0);
  });

  it('refuses a path with a dot segment or outside the API before fetching (D-100 M-3)', async () => {
    const s = stub(json({}));
    configure({ fetch: s.fetch, baseUrl: 'https://api.todayz.localhost' });
    const refused = [
      '/v1/me/orders/../../admin/users',
      '/v1/me/orders/..',
      '/v1/me/./orders',
      '/v1/me/orders/%2e%2e/x',
      '/v1/me/orders/%2E%2E',
      '/v1/me/orders/.%2e/x',
      '/v1',
      '/v2/me',
      '/healthz/../v1/me',
      'v1/me',
      '',
      'https://evil.example/v1/me',
      '//evil.example/v1/me',
    ];
    for (const p of refused) {
      await expect(request(p), p).rejects.toThrow(TypeError);
    }
    expect(s.calls).toHaveLength(0);
    for (const p of ['/v1/me', '/v1/me/orders?cursor=..&limit=20', '/healthz', '/readyz', '/version', '/v1/discover/stores/a.b']) {
      const ok = stub(json({}));
      configure({ fetch: ok.fetch });
      await expect(request(p), p).resolves.toEqual({});
      expect(ok.calls[0]?.url, p).toBe(p);
    }
  });

  it('exposes Retry-After in seconds', async () => {
    configure({
      fetch: stub(
        new Response(JSON.stringify({ type: 't', title: 't', status: 429, code: 'rate_limited' }), {
          status: 429,
          headers: { 'content-type': 'application/problem+json', 'retry-after': '30' },
        }),
      ).fetch,
    });
    const e = (await request('/v1/auth/otp').catch((x: unknown) => x)) as ApiError;
    expect(e.retryAfterSeconds).toBe(30);
    expect(new ApiError(fallbackProblem(503), new Headers()).retryAfterSeconds).toBeUndefined();
    expect(new ApiError(fallbackProblem(503), new Headers({ 'retry-after': 'soon' })).retryAfterSeconds).toBeUndefined();
  });
});

describe('fallbackProblem', () => {
  it('names internal from 500 and bad_request below, both registered codes', () => {
    expect(fallbackProblem(500).code).toBe('internal');
    expect(fallbackProblem(499).code).toBe('bad_request');
    expect(fallbackProblem(404, 'r').requestId).toBe('r');
    expect(fallbackProblem(404, null).requestId).toBeUndefined();
  });

  // The registry comes through `make gen` (src/gen/problems.json, written by scripts/gen.mjs
  // from TODAYZ_PROBLEMS_DIR): shared/ never reads backend/ (D-100 round 2, Low 13). Under
  // `make check` (TODAYZ_CHECK) the file must exist; a bare `pnpm test` before `make gen` skips.
  const registry = problemCodes();
  const registered = registry === undefined && !process.env['TODAYZ_CHECK'] ? it.skip : it;
  registered('uses only codes the registry has (src/gen/problems.json from make gen)', () => {
    if (registry === undefined) throw new Error('src/gen/problems.json missing: run make gen');
    expect(registry).toContain('validation_failed');
    for (const status of [400, 404, 499, 500, 502, 503]) {
      expect(registry, `status ${status}`).toContain(fallbackProblem(status).code);
    }
  });
});

/** The problem codes `make gen` wrote to src/gen/problems.json (the registry, D-24), or undefined before `make gen`. */
function problemCodes(): string[] | undefined {
  const file = resolve(import.meta.dirname, '../src/gen/problems.json');
  if (!existsSync(file)) return undefined;
  const codes: unknown = JSON.parse(readFileSync(file, 'utf8'));
  if (!Array.isArray(codes) || codes.length === 0 || !codes.every((c) => typeof c === 'string')) {
    throw new Error(`${file}: expected a non-empty array of problem codes`);
  }
  return codes as string[];
}
