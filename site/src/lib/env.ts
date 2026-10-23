// Build-time settings. `development` and `e2e` are the only preview modes: they fall back to the
// local API, Cloudflare's published always-pass Turnstile test key and the local app hosts, and they
// show machine-draft locales. Every other mode (production, staging, test, anything new) is gated
// like production: PUBLIC_API_URL and PUBLIC_TURNSTILE_SITE_KEY must be set, and every URL must be a
// bare https origin. scripts/verify-dist.mjs refuses test keys and local hosts unless it runs with --e2e.

/** Cloudflare's published always-pass Turnstile test site key. Never shipped to production. */
export const TURNSTILE_TEST_SITE_KEY = '1x00000000000000000000AA';
export const LOCAL_API_URL = 'https://api.todayz.localhost';
export const DEFAULT_CUSTOMER_APP_URL = 'https://app.todayz.in';
export const DEFAULT_BUSINESS_APP_URL = 'https://business.todayz.in';

/** Build modes that may use local fallbacks and show machine drafts. Every other mode is production. */
export const PREVIEW_MODES = ['development', 'e2e'] as const;

export function isPreviewMode(mode: unknown): boolean {
  return typeof mode === 'string' && (PREVIEW_MODES as readonly string[]).includes(mode);
}

export interface LeadEnv {
  apiUrl: string;
  siteKey: string;
}

export interface AppLinks {
  customerApp: string;
  businessApp: string;
}

type Env = Readonly<Record<string, string | boolean | undefined>>;

function value(env: Env, name: string): string {
  const v = env[name];
  return typeof v === 'string' ? v.trim() : '';
}

/**
 * The value when it is a bare https origin (scheme, host and optional port; no path, query,
 * fragment or credentials), else null. A single trailing slash is accepted and dropped.
 */
export function httpsOrigin(raw: string): string | null {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname !== '/') return null;
  if (!/^https:\/\/[^/?#]+\/?$/i.test(raw)) return null;
  return url.origin;
}

const ORIGIN_HINT = 'must be a bare https origin such as https://api.todayz.in (no path)';

function fail(env: Env, problems: readonly string[]): never {
  throw new Error(`site: a ${String(env['MODE'] ?? 'production')} build ${problems.join('; ')} (use \`build:e2e\` for local and test builds)`);
}

/** An optional origin setting: undefined when unset, the origin when valid; throws when malformed. */
function optionalOrigin(env: Env, name: string): string | undefined {
  const raw = value(env, name);
  if (!raw) return undefined;
  return httpsOrigin(raw) ?? fail(env, [`${name} ${ORIGIN_HINT}`]);
}

/** The lead form's API origin and Turnstile site key. Throws outside preview modes when one is missing or malformed. */
export function resolveLeadEnv(env: Env): LeadEnv {
  const siteKey = value(env, 'PUBLIC_TURNSTILE_SITE_KEY');
  if (isPreviewMode(env['MODE'])) {
    return { apiUrl: optionalOrigin(env, 'PUBLIC_API_URL') ?? LOCAL_API_URL, siteKey: siteKey || TURNSTILE_TEST_SITE_KEY };
  }
  const raw = value(env, 'PUBLIC_API_URL');
  const apiUrl = raw ? httpsOrigin(raw) : null;
  const missing = [!raw && 'PUBLIC_API_URL', !siteKey && 'PUBLIC_TURNSTILE_SITE_KEY'].filter((x): x is string => Boolean(x));
  const problems = [...(missing.length > 0 ? [`needs ${missing.join(' and ')}`] : []), ...(raw && !apiUrl ? [`PUBLIC_API_URL ${ORIGIN_HINT}`] : [])];
  if (problems.length > 0 || !apiUrl) fail(env, problems);
  return { apiUrl, siteKey };
}

/** Customer and business app origins for links. Optional; malformed values fail every build. */
export function resolveAppLinks(env: Env): AppLinks {
  return {
    customerApp: optionalOrigin(env, 'PUBLIC_CUSTOMER_APP_URL') ?? DEFAULT_CUSTOMER_APP_URL,
    businessApp: optionalOrigin(env, 'PUBLIC_BUSINESS_APP_URL') ?? DEFAULT_BUSINESS_APP_URL,
  };
}
