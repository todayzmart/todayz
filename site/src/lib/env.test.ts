// Build-time lead settings: a production build must name the real API and Turnstile key.
import { describe, expect, it } from 'vitest';
import { LOCAL_API_URL, TURNSTILE_TEST_SITE_KEY, httpsOrigin, resolveAppLinks, resolveLeadEnv } from './env.ts';

const REAL = { PUBLIC_API_URL: 'https://api.todayz.in', PUBLIC_TURNSTILE_SITE_KEY: '0x4AAAAAAA-real' };

describe('resolveLeadEnv', () => {
  it('uses the configured values in production', () => {
    expect(resolveLeadEnv({ MODE: 'production', ...REAL })).toEqual({ apiUrl: 'https://api.todayz.in', siteKey: '0x4AAAAAAA-real' });
  });

  it.each([
    [{ PUBLIC_TURNSTILE_SITE_KEY: '0x4AAAAAAA-real' }, /PUBLIC_API_URL/],
    [{ PUBLIC_API_URL: 'https://api.todayz.in' }, /PUBLIC_TURNSTILE_SITE_KEY/],
    [{ PUBLIC_API_URL: '', PUBLIC_TURNSTILE_SITE_KEY: '   ' }, /PUBLIC_API_URL and PUBLIC_TURNSTILE_SITE_KEY/],
    [{ PUBLIC_API_URL: true, PUBLIC_TURNSTILE_SITE_KEY: '0x4AAAAAAA-real' }, /PUBLIC_API_URL/],
    [{}, /PUBLIC_API_URL and PUBLIC_TURNSTILE_SITE_KEY/],
  ])('fails a production build when a value is missing or empty: %o', (env, message) => {
    expect(() => resolveLeadEnv({ MODE: 'production', ...env })).toThrow(message);
  });

  it.each(['staging', 'test', 'preview', ''])('gates the %s mode like production', (MODE) => {
    expect(() => resolveLeadEnv({ MODE })).toThrow(/PUBLIC_API_URL and PUBLIC_TURNSTILE_SITE_KEY/);
    expect(() => resolveLeadEnv({})).toThrow(/PUBLIC_API_URL/);
  });

  it.each(['https://api.todayz.in/v1', 'http://api.todayz.in', 'https://api.todayz.in?x=1', 'https://u:p@api.todayz.in', 'api.todayz.in', 'https://api.todayz.in//'])(
    'refuses PUBLIC_API_URL %s (a bare https origin only)',
    (url) => {
      expect(() => resolveLeadEnv({ MODE: 'production', ...REAL, PUBLIC_API_URL: url })).toThrow(/bare https origin/);
      expect(() => resolveLeadEnv({ MODE: 'e2e', PUBLIC_API_URL: url })).toThrow(/bare https origin/);
    },
  );

  it('accepts an origin with a port or one trailing slash, and returns the origin', () => {
    expect(httpsOrigin('https://api.todayz.in/')).toBe('https://api.todayz.in');
    expect(httpsOrigin('https://api.todayz.localhost:8443')).toBe('https://api.todayz.localhost:8443');
  });

  it.each(['e2e', 'development'])('falls back to the local API and the test key in %s mode', (MODE) => {
    expect(resolveLeadEnv({ MODE })).toEqual({ apiUrl: LOCAL_API_URL, siteKey: TURNSTILE_TEST_SITE_KEY });
    expect(resolveLeadEnv({ MODE, ...REAL })).toEqual({ apiUrl: 'https://api.todayz.in', siteKey: '0x4AAAAAAA-real' });
  });

  it('trims the values', () => {
    expect(resolveLeadEnv({ MODE: 'production', PUBLIC_API_URL: ' https://api.todayz.in ', PUBLIC_TURNSTILE_SITE_KEY: ' k ' })).toEqual({
      apiUrl: 'https://api.todayz.in',
      siteKey: 'k',
    });
  });
});

describe('resolveAppLinks', () => {
  it('defaults to the production apps and takes local hosts from the env', () => {
    expect(resolveAppLinks({ MODE: 'production' })).toEqual({ customerApp: 'https://app.todayz.in', businessApp: 'https://business.todayz.in' });
    expect(
      resolveAppLinks({ MODE: 'e2e', PUBLIC_CUSTOMER_APP_URL: 'https://app.todayz.localhost', PUBLIC_BUSINESS_APP_URL: 'https://business.todayz.localhost/' }),
    ).toEqual({ customerApp: 'https://app.todayz.localhost', businessApp: 'https://business.todayz.localhost' });
  });

  it('refuses a malformed app URL', () => {
    expect(() => resolveAppLinks({ MODE: 'production', PUBLIC_CUSTOMER_APP_URL: 'https://app.todayz.in/home' })).toThrow(/PUBLIC_CUSTOMER_APP_URL/);
  });
});
