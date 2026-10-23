// Build-time settings for the lead form. A production build must name the real API origin and
// Turnstile site key; other modes (dev, e2e) fall back to the local API and Cloudflare's published
// always-pass test key. scripts/verify-dist.mjs refuses that test key unless it runs with --e2e.

/** Cloudflare's published always-pass Turnstile test site key. Never shipped to production. */
export const TURNSTILE_TEST_SITE_KEY = '1x00000000000000000000AA';
export const LOCAL_API_URL = 'https://api.todayz.localhost';

export interface LeadEnv {
  apiUrl: string;
  siteKey: string;
}

type Env = Readonly<Record<string, string | boolean | undefined>>;

function value(env: Env, name: string): string {
  const v = env[name];
  return typeof v === 'string' ? v.trim() : '';
}

/** Throws in production when PUBLIC_API_URL or PUBLIC_TURNSTILE_SITE_KEY is missing or empty. */
export function resolveLeadEnv(env: Env): LeadEnv {
  const apiUrl = value(env, 'PUBLIC_API_URL');
  const siteKey = value(env, 'PUBLIC_TURNSTILE_SITE_KEY');
  if (env['MODE'] === 'production') {
    const missing = [!apiUrl && 'PUBLIC_API_URL', !siteKey && 'PUBLIC_TURNSTILE_SITE_KEY'].filter(Boolean);
    if (missing.length > 0) {
      throw new Error(`site: a production build needs ${missing.join(' and ')} (use \`build:e2e\` for local and test builds)`);
    }
    return { apiUrl, siteKey };
  }
  return { apiUrl: apiUrl || LOCAL_API_URL, siteKey: siteKey || TURNSTILE_TEST_SITE_KEY };
}
