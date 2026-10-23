// The generated half (src/gen, src/mocks/handlers.gen.ts): present after make gen.
// The client calls go through request(); the Zod schemas enforce the contract's
// shapes; the MSW handlers answer the examples. Everything here resolves the
// generated modules, so run `make gen` (or `pnpm gen`) first. The fetch stubs answer with
// `application/json` as the API does: request() resolves by Content-Type (D-100 round 2, Low 10).
// @vitest-environment node
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { ApiError, configure } from '../src';
import { getHealthz, getVersion, moveTestClock } from '../src/gen/platform/platform';
import { GetHealthzResponse, MoveTestClockBody } from '../src/gen/platform/platform.zod';
import { examples, handlers, problemResponse } from '../src/mocks';

import { http } from 'msw';

const server = setupServer(...handlers);

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  configure({});
});
afterAll(() => server.close());

describe('generated client through request()', () => {
  it('GETs the operation path and returns the typed body', async () => {
    const fetch = vi.fn(async (_url: string) => new Response(JSON.stringify({ status: 'ok' }), { status: 200, headers: { 'content-type': 'application/json' } }));
    configure({ fetch: fetch as unknown as typeof globalThis.fetch, baseUrl: 'https://api.todayz.localhost' });
    const health = await getHealthz();
    expect(health.status).toBe('ok');
    expect(fetch.mock.calls[0]?.[0]).toBe('https://api.todayz.localhost/healthz');
  });

  it('POSTs a JSON body with the request options', async () => {
    const calls: RequestInit[] = [];
    const fetch = vi.fn(async (_url: string, init?: RequestInit) => {
      calls.push(init ?? {});
      return new Response(JSON.stringify({ now: '2026-10-22T10:01:00Z', offsetMs: 1860000 }), { status: 200, headers: { 'content-type': 'application/json' } });
    });
    configure({ fetch: fetch as unknown as typeof globalThis.fetch });
    const state = await moveTestClock({ advance: '31m' }, { idempotencyKey: '01J0000000000000000000000X' });
    expect(state.offsetMs).toBe(1860000);
    expect(calls[0]?.method).toBe('POST');
    expect(calls[0]?.body).toBe('{"advance":"31m"}');
    const h = new Headers(calls[0]?.headers);
    expect(h.get('content-type')).toBe('application/json');
    expect(h.get('idempotency-key')).toBe('01J0000000000000000000000X');
  });
});

describe('MSW handlers from the examples', () => {
  // Node's fetch needs an absolute URL; the handlers match any origin.
  beforeAll(() => configure({ baseUrl: 'https://api.todayz.localhost' }));

  it('answers every operation with its example', async () => {
    const version = await getVersion();
    expect(version).toEqual(examples.getVersion.body);
    expect(examples.getHealthz.method).toBe('GET');
    expect(handlers.length).toBeGreaterThanOrEqual(5);
  });

  it('lets a test override one answer with a problem', async () => {
    configure({ baseUrl: 'https://api.todayz.localhost' });
    server.use(http.get('*/version', () => problemResponse('not_found', 404, { detail: 'gone' })));
    const e = (await getVersion().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.code).toBe('not_found');
    expect(e.status).toBe(404);
    expect(e.headers.get('content-type')).toBe('application/problem+json');
  });
});

describe('Zod schemas', () => {
  it('accepts a body the contract allows and refuses one it does not', () => {
    expect(MoveTestClockBody.safeParse({ advance: '31m' }).success).toBe(true);
    expect(MoveTestClockBody.safeParse({}).success).toBe(false);
    expect(MoveTestClockBody.safeParse({ advance: 31 }).success).toBe(false);
  });

  it('checks a response shape', () => {
    expect(GetHealthzResponse.safeParse({ status: 'ok' }).success).toBe(true);
    expect(GetHealthzResponse.safeParse({ status: 'meh' }).success).toBe(false);
  });
});
