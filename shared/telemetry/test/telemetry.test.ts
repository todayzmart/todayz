import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  configFromEnv,
  initTelemetry,
  routeTemplate,
  rumAllowed,
  scrub,
  type RumSample,
  type TelemetryConfig,
} from '../src/index';

const base: TelemetryConfig = { app: 'customer', env: 'production', rumEndpoint: 'https://api.todayz.in/v1/rum' };

describe('rumAllowed', () => {
  it.each<[string, Partial<TelemetryConfig>, boolean]>([
    ['production with endpoint', {}, true],
    ['no endpoint', { rumEndpoint: undefined }, false],
    ['test env', { env: 'test' }, false],
    ['local env', { env: 'local' }, false],
    ['local env, forced', { env: 'local', forceLocalRum: true }, true],
    ['bad url', { rumEndpoint: 'not a url' }, false],
    ['non-http url', { rumEndpoint: 'javascript:alert(1)' }, false],
  ])('%s', (_name, patch, want) => {
    expect(rumAllowed({ ...base, ...patch })).toBe(want);
  });
});

describe('configFromEnv', () => {
  it('is off with an empty env', () => {
    const c = configFromEnv('admin', { MODE: 'development' });
    expect(c).toMatchObject({ app: 'admin', env: 'local' });
    expect(rumAllowed(c)).toBe(false);
  });

  it('treats vitest as test and ignores a Sentry DSN', () => {
    const c = configFromEnv('business', { MODE: 'test', VITE_SENTRY_DSN: 'https://k@example.invalid/1' });
    expect(c.env).toBe('test');
    expect(initTelemetry(c).sentryEnabled).toBe(false);
  });
});

describe('initTelemetry', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('makes no network calls when off', () => {
    const fetchSpy = vi.fn();
    const beacon = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon });
    const t = initTelemetry({ app: 'customer', env: 'local' });
    t.captureError(new Error('boom'));
    expect(t.rumEnabled).toBe(false);
    expect(t.flush()).toBe(0);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(beacon).not.toHaveBeenCalled();
  });

  it('batches errors to the endpoint with sendBeacon when on', () => {
    const beacon = vi.fn(() => true);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon });
    const t = initTelemetry(base);
    t.captureError(new Error('boom'), { where: 'test' });
    expect(t.flush()).toBe(1);
    expect(beacon).toHaveBeenCalledWith('https://api.todayz.in/v1/rum', expect.any(Blob));
    t.shutdown();
  });
});

describe('routeTemplate', () => {
  it('uses the matched route id when the router gives one', () => {
    expect(routeTemplate('/r/k7Qp2xZ9aB', '/r/$token')).toBe('/r/$token');
    expect(routeTemplate('/orders', '/orders/')).toBe('/orders');
    expect(routeTemplate('/', '/')).toBe('/');
    // The root match id is not a path; fall back to the pathname.
    expect(routeTemplate('/orders/123', '__root__')).toBe('/orders/{id}');
  });

  it.each([
    ['digits', '/stores/123/orders', '/stores/{id}/orders'],
    ['ULID', '/orders/01HZX5KQ7M4N8P2R6S9T3V5W7Y', '/orders/{id}'],
    ['UUID', '/u/3f2b8c1e-9a4d-4e6f-8b7a-1c2d3e4f5a6b', '/u/{id}'],
    ['longer than 20 chars', '/r/abcdefghijklmnopqrstuv', '/r/{id}'],
    ['letters and digits, 12+', '/r/k7Qp2xZ9aBcD', '/r/{id}'],
    ['a /r/<token> share link', '/r/Xy7Kp2Qz9LmN4', '/r/{id}'],
  ])('replaces %s with {id}', (_name, path, want) => {
    expect(routeTemplate(path)).toBe(want);
  });

  it('keeps ordinary page segments', () => {
    expect(routeTemplate('/stores/near-me/orders')).toBe('/stores/near-me/orders');
    expect(routeTemplate('/settings/notifications')).toBe('/settings/notifications');
    // Short mixed segments are page names, not ids.
    expect(routeTemplate('/help/faq2025')).toBe('/help/faq2025');
  });
});

describe('scrub', () => {
  it.each([
    ['+91 phone', 'call +91 98765 43210 now', 'call {phone} now'],
    ['+91 phone, no space', 'from +919876543210', 'from {phone}'],
    ['0-prefixed phone', 'dial 09876543210', 'dial {phone}'],
    ['bare mobile', 'user 9876543210 failed', 'user {phone} failed'],
    ['OTP', 'OTP 482913 rejected', 'OTP {code} rejected'],
    ['4-digit pickup code', 'code 4821 wrong', 'code {code} wrong'],
    ['8-digit code', 'ref 12345678', 'ref {code}'],
    ['long number', 'acct 123456789012', 'acct {number}'],
    ['email', 'no user ravi.k+shop@example.co.in', 'no user {email}'],
    ['bearer token', 'Authorization: Bearer abc.DEF-123_x', 'Authorization: Bearer {token}'],
    ['JWT', 'bad eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig-PART_x', 'bad {jwt}'],
    ['opaque token', 'key k3J9xQ2mZp8LwR5tY7uV1nB4cD6eF0gH', 'key {token}'],
  ])('removes a %s', (_name, input, want) => {
    expect(scrub(input)).toBe(want);
  });

  it('leaves ordinary messages alone', () => {
    expect(scrub('Cannot read properties of undefined (reading "map")')).toBe('Cannot read properties of undefined (reading "map")');
    expect(scrub('timeout after 30s on v1.2.3')).toBe('timeout after 30s on v1.2.3');
  });

  it('is applied to captured errors and their context', () => {
    const beacon = vi.fn((_url: string, _body: Blob) => true);
    vi.stubGlobal('navigator', { ...navigator, sendBeacon: beacon });
    const t = initTelemetry({ ...base, routeId: () => '/r/$token' });
    t.captureError(new Error('OTP 482913 for +91 98765 43210'), { user: 'ravi@example.com' });
    t.flush();
    t.shutdown();
    vi.unstubAllGlobals();
    const blob = beacon.mock.calls[0]?.[1];
    expect(blob).toBeInstanceOf(Blob);
    return blob?.text().then((body) => {
      const sample = (JSON.parse(body) as { samples: RumSample[] }).samples[0];
      expect(sample).toMatchObject({
        type: 'error',
        route: '/r/$token',
        detail: { message: 'OTP {code} for {phone}', user: '{email}' },
      });
      expect(body).not.toMatch(/482913|98765|ravi@/);
    });
  });
});
