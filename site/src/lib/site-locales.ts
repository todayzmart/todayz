// The locales this build ships, from the Vite mode: every locale in `development` and `e2e`
// (machine drafts show with a banner and noindex), English plus fully reviewed locales in every
// other mode. Vite-only (import.meta.env); Node scripts call siteLocales(mode) themselves.
import { siteLocales } from '../i18n/index.ts';

export const SITE_LOCALES = siteLocales(import.meta.env.MODE);
