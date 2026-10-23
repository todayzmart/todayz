import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  anonId,
  createEmitter,
  initTelemetry,
  newTraceparent,
  newUlid,
  scrub,
  scrubSentryEvent,
  sentryAllowed,
  sessionId,
  TRACEPARENT_PATTERN,
  tracedFetch,
  type SentryLike,
} from '../src/index';

const ULID = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/;

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('traceparent', () => {
  it('is W3C version 00, 32 hex trace id, 16 hex span id, sampled', () => {
    for (let i = 0; i < 20; i++) expect(newTraceparent()).toMatch(TRACEPARENT_PATTERN);
    expect(newTraceparent()).not.toBe(newTraceparent());
  });

  it('is added to our API origins only, and never overwrites a caller header', async () => {
    const base = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('{}', { status: 200 }));
    const timings: { method: string; path: string; status: number }[] = [];
    const f = tracedFetch(base as unknown as typeof fetch, {
      apiOrigins: ['https://api.todayz.in'],
      pageOrigin: 'https://app.todayz.in',
      onTiming: (t) => timings.push({ method: t.method, path: t.path, status: t.status }),
    });
    await f('https://api.todayz.in/v1/me?x=1');
    await f('https://cdn.example.com/lib.js');
    await f('https://api.todayz.in/v1/x', { headers: { traceparent: '00-' + 'a'.repeat(32) + '-' + 'b'.repeat(16) + '-01' } });

    const hdr = (i: number): Headers => new Headers(base.mock.calls[i]?.[1]?.headers);
    expect(hdr(0).get('traceparent')).toMatch(TRACEPARENT_PATTERN);
    expect(base.mock.calls[1]?.[1]).toBeUndefined();
    expect(hdr(2).get('traceparent')).toBe('00-' + 'a'.repeat(32) + '-' + 'b'.repeat(16) + '-01');
    expect(timings).toEqual([
      { method: 'GET', path: '/v1/me', status: 200 },
      { method: 'GET', path: '/v1/x', status: 200 },
    ]);
  });
});

describe('ids', () => {
  it('makes contract-shaped ULIDs that sort by time', () => {
    const a = newUlid(1_700_000_000_000);
    const b = newUlid(1_700_000_001_000);
    expect(a).toMatch(ULID);
    expect(a < b).toBe(true);
    expect(newUlid(0)).toMatch(ULID);
  });

  it('keeps the anonymous id in localStorage and the session id in sessionStorage', () => {
    const store = (): Storage => {
      const m = new Map<string, string>();
      return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v) } as Storage;
    };
    vi.stubGlobal('localStorage', store());
    vi.stubGlobal('sessionStorage', store());
    expect(anonId()).toBe(anonId());
    expect(anonId()).toMatch(ULID);
    expect(sessionId()).toMatch(ULID);
    expect(sessionId()).not.toBe(anonId());
  });

  it('survives storage that throws', () => {
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    vi.stubGlobal('localStorage', broken);
    expect(anonId()).toMatch(ULID);
    expect(anonId()).toBe(anonId());
  });
});

describe('createEmitter', () => {
  const on = { app: 'customer', env: 'production', endpoint: 'https://api.todayz.in/v1/events' } as const;
  let beacon: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    beacon = vi.fn((_url: string, _blob: Blob) => true);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon });
  });

  const sent = async (call = 0): Promise<Record<string, unknown>> => {
    const blob = beacon.mock.calls[call]?.[1] as Blob;
    return JSON.parse(await blob.text()) as Record<string, unknown>;
  };

  it('sends nothing locally, in tests, or without an endpoint', () => {
    for (const cfg of [{ ...on, env: 'local' as const }, { ...on, env: 'test' as const }, { ...on, endpoint: undefined }]) {
      const e = createEmitter(cfg);
      e.emit('pwa_installed');
      expect(e.enabled).toBe(false);
      expect(e.flush()).toBe(0);
    }
    expect(beacon).not.toHaveBeenCalled();
  });

  it('batches typed events with ids, time, anon id and app version', async () => {
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => undefined });
    const e = createEmitter({ ...on, release: 'customer@3f2a9c1', now: () => Date.UTC(2026, 9, 23, 10) });
    e.emit('app_opened', { launch: 'cold', source: 'push' }, { geoCell: '88618a1b3dfffff' });
    e.emit('pwa_installed');
    expect(e.flush()).toBe(2);
    expect(beacon).toHaveBeenCalledWith('https://api.todayz.in/v1/events', expect.any(Blob));
    const body = await sent();
    expect(body).toMatchObject({ app: 'customer', platform: 'web', appVersion: '3f2a9c1' });
    expect(body['anonId']).toMatch(ULID);
    expect(body['sessionId']).toMatch(ULID);
    const events = body['events'] as { id: string; name: string; t: string; props?: unknown; geoCell?: string }[];
    expect(events.map((x) => x.name)).toEqual(['app_opened', 'pwa_installed']);
    expect(events[0]).toMatchObject({ t: '2026-10-23T10:00:00.000Z', props: { launch: 'cold', source: 'push' }, geoCell: '88618a1b3dfffff' });
    expect(events[0]?.id).toMatch(ULID);
    expect(events[1]).not.toHaveProperty('props');
    e.shutdown();
  });

  it('drops a malformed geo cell instead of sending coordinates', async () => {
    const e = createEmitter(on);
    e.emit('session_started', {}, { geoCell: '12.9716,77.5946' });
    e.flush();
    expect(JSON.stringify(await sent())).not.toContain('77.5946');
    e.shutdown();
  });

  it('flushes after the delay and when the page is hidden', () => {
    vi.useFakeTimers();
    const e = createEmitter({ ...on, flushMs: 1000 });
    e.emit('session_started');
    expect(beacon).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1000);
    expect(beacon).toHaveBeenCalledTimes(1);

    e.emit('session_started');
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(beacon).toHaveBeenCalledTimes(2);
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
    e.shutdown();
  });

  it('splits more than 50 events into batches of 50', async () => {
    const e = createEmitter(on);
    for (let i = 0; i < 120; i++) e.emit('session_started');
    expect(e.flush()).toBe(20);
    expect(beacon).toHaveBeenCalledTimes(3);
    expect(((await sent(0))['events'] as unknown[]).length).toBe(50);
    e.shutdown();
  });
});

describe('sentry seam', () => {
  const sdk = (): { init: ReturnType<typeof vi.fn>; captureException: ReturnType<typeof vi.fn> } & SentryLike => {
    const init = vi.fn();
    const captureException = vi.fn();
    return { init, captureException } as unknown as { init: typeof init; captureException: typeof captureException } & SentryLike;
  };

  it('needs staging or production, a DSN and an injected SDK', () => {
    const s = sdk();
    expect(sentryAllowed('production', 'https://k@example.invalid/1', s)).toBe(true);
    expect(sentryAllowed('staging', 'https://k@example.invalid/1', s)).toBe(true);
    expect(sentryAllowed('local', 'https://k@example.invalid/1', s)).toBe(false);
    expect(sentryAllowed('test', 'https://k@example.invalid/1', s)).toBe(false);
    expect(sentryAllowed('production', undefined, s)).toBe(false);
    expect(sentryAllowed('production', 'https://k@example.invalid/1', undefined)).toBe(false);
  });

  it('is not initialised locally even with a DSN and an SDK', () => {
    const s = sdk();
    const t = initTelemetry({ app: 'customer', env: 'local', sentryDsn: 'https://k@example.invalid/1', sentry: s });
    t.captureError(new Error('boom'));
    expect(t.sentryEnabled).toBe(false);
    expect(s.init).not.toHaveBeenCalled();
    expect(s.captureException).not.toHaveBeenCalled();
  });

  it('initialises with PII off and reports scrubbed context in production', () => {
    const s = sdk();
    const t = initTelemetry({ app: 'business', env: 'production', release: 'business@abc', sentryDsn: 'https://k@example.invalid/1', sentry: s });
    expect(t.sentryEnabled).toBe(true);
    expect(s.init).toHaveBeenCalledWith(expect.objectContaining({ environment: 'production', release: 'business@abc', sendDefaultPii: false }));
    t.captureError(new Error('OTP 482913 failed'), { phone: '9876543210' });
    const extra = (s.captureException.mock.calls[0]?.[1] as { extra: Record<string, string> }).extra;
    expect(extra['message']).toBe('OTP {code} failed');
    expect(extra['phone']).toBe('{phone}');
    t.shutdown();
  });

  it('beforeSend keeps only the user id and strips the request and breadcrumb data', () => {
    const out = scrubSentryEvent(
      {
        message: 'call 9876543210',
        user: { id: '01JB0S1W6W8ZV7T1C3A6M9N4PQ', email: 'a@b.in', ip_address: '1.2.3.4' },
        request: { url: 'https://app.todayz.in/r/abc?code=482913', cookies: 'x', headers: { a: 'b' } },
        exception: { values: [{ value: 'OTP 482913' }] },
        breadcrumbs: [{ message: 'tap 9876543210', data: { phone: '9876543210' } }],
      },
      scrub,
    );
    expect(out.user).toEqual({ id: '01JB0S1W6W8ZV7T1C3A6M9N4PQ' });
    expect(out.request).toEqual({ url: 'https://app.todayz.in/r/abc' });
    expect(out.message).toBe('call {phone}');
    expect(out.exception?.values?.[0]?.value).toBe('OTP {code}');
    expect(out.breadcrumbs).toEqual([{ message: 'tap {phone}' }]);
  });
});

describe('RUM batch shape', () => {
  it('carries platform, conn and a release the contract accepts', async () => {
    const beacon = vi.fn((_url: string, _blob: Blob) => true);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon, connection: { effectiveType: '3g' } });
    const t = initTelemetry({ app: 'customer', env: 'production', rumEndpoint: 'https://api.todayz.in/v1/rum', release: 'customer@' + 'x'.repeat(80) });
    t.captureError(new Error('boom'));
    t.flush();
    t.shutdown();
    const body = JSON.parse(await (beacon.mock.calls[0]?.[1] as Blob).text()) as { release: string; platform: string; conn: string };
    expect(body).toMatchObject({ platform: 'web', conn: '3g' });
    expect(body.release.length).toBeLessThanOrEqual(64);
  });

  it('records a route_change timing between routeStart and routeReady', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    });
    const beacon = vi.fn((_url: string, _blob: Blob) => true);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon });
    const t = initTelemetry({ app: 'customer', env: 'production', rumEndpoint: 'https://api.todayz.in/v1/rum' });
    t.routeReady('/search'); // no start: ignored
    t.routeStart();
    t.routeReady('/search');
    t.flush();
    t.shutdown();
    const body = JSON.parse(await (beacon.mock.calls[0]?.[1] as Blob).text()) as { samples: { name: string; route: string; value: number }[] };
    const sample = body.samples.find((s) => s.name === 'route_change');
    expect(sample).toMatchObject({ route: '/search' });
    expect(sample?.value).toBeGreaterThanOrEqual(0);
    expect(body.samples.filter((s) => s.name === 'route_change')).toHaveLength(1);
  });
});
