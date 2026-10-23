// Build-time settings. vite.config.ts `define`s both (shared/auth/build.mjs); a production build
// fails without a real Turnstile site key and an https API URL.
export const API_URL: string = import.meta.env.VITE_API_URL ?? 'https://api.todayz.localhost';
export const TURNSTILE_SITE_KEY: string = import.meta.env.VITE_TURNSTILE_SITE_KEY ?? '1x00000000000000000000AA';
/** Sent as X-TZ-Client with every auth call (AU-16). */
export const CLIENT_ID = 'admin/0.0.0';
