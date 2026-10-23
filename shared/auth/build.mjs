// Build-time settings for the three Vite apps (W1-08), plain .mjs so vite.config.ts can import it
// without a TypeScript loader:
//
//   import { authDefines } from '@todayz/auth/build';
//   export default defineConfig(({ mode }) => ({ define: authDefines(mode, loadEnv(mode, process.cwd(), 'VITE_')) }));
//
// Only the modes `development`, `test` and `e2e` fall back to the local API and Cloudflare's
// published always-pass test key (which sends the dummy token and loads no script). Every other
// mode (`production`, `staging`, a mistyped one) needs a real Turnstile site key and an https API
// URL and fails without them.

export const TURNSTILE_TEST_SITE_KEY = '1x00000000000000000000AA';
export const LOCAL_API_URL = 'https://api.todayz.localhost';

/** The only modes allowed to build with the test site key and the local API URL. */
export const DEV_MODES = new Set(['development', 'test', 'e2e']);

const TEST_KEY = /^[123]x0{20,}[A-Za-z]{2}$/;

/** Cloudflare's always-pass, always-block and forced-challenge test site keys. */
export function isTestSiteKey(key) {
  return TEST_KEY.test(key);
}

/**
 * @param {string} mode Vite mode
 * @param {Record<string, string | undefined>} env the VITE_ variables (loadEnv)
 * @returns {Record<string, string>} Vite `define` entries
 */
export function authDefines(mode, env) {
  const key = (env.VITE_TURNSTILE_SITE_KEY ?? '').trim();
  const api = (env.VITE_API_URL ?? '').trim().replace(/\/+$/, '');
  if (!DEV_MODES.has(mode)) {
    if (key === '' || isTestSiteKey(key)) {
      throw new Error(`VITE_TURNSTILE_SITE_KEY must be a real Turnstile site key for a build in mode "${mode}"`);
    }
    if (!/^https:\/\/[^/]+$/.test(api)) {
      throw new Error(`VITE_API_URL must be the https origin of the API for a build in mode "${mode}"`);
    }
  }
  return {
    'import.meta.env.VITE_TURNSTILE_SITE_KEY': JSON.stringify(key === '' ? TURNSTILE_TEST_SITE_KEY : key),
    'import.meta.env.VITE_API_URL': JSON.stringify(api === '' ? LOCAL_API_URL : api),
  };
}
