// Build-time settings. vite.config.ts `define`s both (shared/auth/build.mjs); a production build
// fails without a real Turnstile site key and an https API URL.
export const API_URL: string = import.meta.env.VITE_API_URL ?? 'https://api.todayz.localhost';
export const TURNSTILE_SITE_KEY: string = import.meta.env.VITE_TURNSTILE_SITE_KEY ?? '1x00000000000000000000AA';
/** Sent as X-TZ-Client with every auth call (AU-16). */
export const CLIENT_ID = 'customer/0.0.0';
/**
 * Where product images are served (img.todayz.localhost locally, W1-05's image bucket). `imageKey` from the API is the
 * object path under it. Empty means no image host is configured: tiles keep their initials.
 */
export const IMAGE_BASE_URL: string = (import.meta.env.VITE_IMG_URL ?? 'https://img.todayz.localhost').replace(/\/+$/, '');
/** Protomaps basemap file for the StoreMap (tiles.todayz.localhost locally). */
export const TILES_URL: string = (import.meta.env.VITE_TILES_URL ?? 'https://tiles.todayz.localhost').replace(/\/+$/, '');
