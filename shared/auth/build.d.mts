export const TURNSTILE_TEST_SITE_KEY: string;
export const LOCAL_API_URL: string;
export function isTestSiteKey(key: string): boolean;
export function authDefines(mode: string, env: Record<string, string | undefined>): Record<string, string>;
