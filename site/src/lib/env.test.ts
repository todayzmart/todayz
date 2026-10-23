// Build-time lead settings: a production build must name the real API and Turnstile key.
import { describe, expect, it } from 'vitest';
import { LOCAL_API_URL, TURNSTILE_TEST_SITE_KEY, resolveLeadEnv } from './env.ts';

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

  it.each(['e2e', 'development', 'test'])('falls back to the local API and the test key in %s mode', (MODE) => {
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
