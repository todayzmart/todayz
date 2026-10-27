// Build-time settings. vite.config.ts `define`s both (shared/auth/build.mjs); a production build
// fails without a real Turnstile site key and an https API URL.
export const API_URL: string = import.meta.env.VITE_API_URL ?? 'https://api.todayz.localhost';
export const TURNSTILE_SITE_KEY: string = import.meta.env.VITE_TURNSTILE_SITE_KEY ?? '1x00000000000000000000AA';
/** Sent as X-TZ-Client with every auth call (AU-16). */
export const CLIENT_ID = 'admin/0.0.0';
/**
 * Where uploaded images are served (img.todayz.localhost locally, W1-05's image bucket); an API `imageKey` is the
 * object path under it. Empty means no image host is configured: banners show their headline only.
 */
export const IMAGE_BASE_URL: string = (import.meta.env.VITE_IMG_URL ?? 'https://img.todayz.localhost').replace(/\/+$/, '');
/**
 * Grafana, for A-21's Explore links and A-22's dashboard links (grafana.todayz.localhost locally).
 * A production build without `VITE_GRAFANA_URL` has no Grafana: the links are left out.
 */
export const GRAFANA_URL: string = (import.meta.env.VITE_GRAFANA_URL ?? (import.meta.env.MODE === 'production' ? '' : 'https://grafana.todayz.localhost')).replace(
  /\/+$/,
  '',
);
