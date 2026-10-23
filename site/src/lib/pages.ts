// The page registry: one row per page, shared by routes, the sitemap, hreflang, the footer and the
// build checks. Plain TypeScript with no Vite-only features, so Node scripts can import it too.
import { DEFAULT_LOCALE, LOCALES, OTHER_LOCALES, type Locale, type MessageKey } from '../i18n/index.ts';

export const SITE_ORIGIN = 'https://todayz.in';

export type PageId =
  | 'home'
  | 'retailers'
  | 'how'
  | 'pricing'
  | 'about'
  | 'contact'
  | 'privacy'
  | 'terms-shoppers'
  | 'terms-shops'
  | 'refunds';

export interface PageDef {
  id: PageId;
  /** English path, always with a trailing slash (the site builds directory URLs). Other locales prefix it with /<locale>. */
  path: string;
  /** Locales this page exists in. English always; hi, kn, te and ta only for home and retailers (S-05). */
  locales: readonly Locale[];
  title: MessageKey;
  description: MessageKey;
  /** Page holds the lead form, so it loads the Turnstile script. */
  leadForm: boolean;
  /** Priority hint for the sitemap. */
  priority: number;
}

const EN_ONLY = [DEFAULT_LOCALE] as const;

export const PAGES: readonly PageDef[] = [
  { id: 'home', path: '/', locales: LOCALES, title: 'home.meta.title', description: 'home.meta.description', leadForm: true, priority: 1 },
  { id: 'retailers', path: '/for-retailers/', locales: LOCALES, title: 'retailers.meta.title', description: 'retailers.meta.description', leadForm: true, priority: 0.9 },
  { id: 'how', path: '/how-it-works/', locales: EN_ONLY, title: 'info.how.meta.title', description: 'info.how.meta.description', leadForm: false, priority: 0.7 },
  { id: 'pricing', path: '/pricing/', locales: EN_ONLY, title: 'info.pricing.meta.title', description: 'info.pricing.meta.description', leadForm: false, priority: 0.7 },
  { id: 'about', path: '/about/', locales: EN_ONLY, title: 'info.about.meta.title', description: 'info.about.meta.description', leadForm: false, priority: 0.5 },
  { id: 'contact', path: '/contact/', locales: EN_ONLY, title: 'info.contact.meta.title', description: 'info.contact.meta.description', leadForm: true, priority: 0.5 },
  { id: 'privacy', path: '/legal/privacy/', locales: EN_ONLY, title: 'legal.privacy.meta.title', description: 'legal.privacy.meta.description', leadForm: false, priority: 0.3 },
  { id: 'terms-shoppers', path: '/legal/terms-for-shoppers/', locales: EN_ONLY, title: 'legal.terms_shoppers.meta.title', description: 'legal.terms_shoppers.meta.description', leadForm: false, priority: 0.3 },
  { id: 'terms-shops', path: '/legal/terms-for-shops/', locales: EN_ONLY, title: 'legal.terms_shops.meta.title', description: 'legal.terms_shops.meta.description', leadForm: false, priority: 0.3 },
  { id: 'refunds', path: '/legal/refunds-and-credits/', locales: EN_ONLY, title: 'legal.refunds.meta.title', description: 'legal.refunds.meta.description', leadForm: false, priority: 0.3 },
];

export function pageById(id: PageId): PageDef {
  const page = PAGES.find((p) => p.id === id);
  if (!page) throw new Error(`unknown page ${id}`);
  return page;
}

/** URL path of a page in a locale, always ending in a slash: "/", "/hi/", "/for-retailers/", "/hi/for-retailers/". */
export function pagePath(page: PageDef, locale: Locale): string {
  return locale === DEFAULT_LOCALE ? page.path : `/${locale}${page.path}`;
}

export function pageUrl(page: PageDef, locale: Locale): string {
  return `${SITE_ORIGIN}${pagePath(page, locale)}`;
}

/** hreflang entries for a page: every locale it exists in plus x-default (English). */
export function alternates(page: PageDef): { hreflang: string; href: string }[] {
  if (page.locales.length < 2) return [];
  return [
    ...page.locales.map((l) => ({ hreflang: l, href: pageUrl(page, l) })),
    { hreflang: 'x-default', href: pageUrl(page, DEFAULT_LOCALE) },
  ];
}

/** Every (page, locale) pair that gets built. */
export function allRoutes(): { page: PageDef; locale: Locale }[] {
  return PAGES.flatMap((page) => page.locales.map((locale) => ({ page, locale })));
}

export { OTHER_LOCALES };
