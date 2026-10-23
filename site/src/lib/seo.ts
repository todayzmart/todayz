// Sitemap, robots and the old-URL redirect map as pure functions so tests and endpoints share them.
import type { Locale } from '../i18n/index.ts';
import { allRoutes, alternates, pageUrl, SITE_ORIGIN } from './pages.ts';

/** Old todayz.in paths and where they go (301). Anchors cannot be redirected, so only paths appear. */
export const REDIRECTS: readonly { from: string; to: string }[] = [
  // The old site was one landing page behind an iframe wrapper; Pages already serves /index.html as /.
  { from: '/todayz-landing-page.html', to: '/' },
];

/** Sitemap of every route a build with these locales produces (siteLocales). */
export function sitemapXml(site: readonly Locale[]): string {
  const entries = allRoutes(site).map(({ page, locale }) => {
    const alts = alternates(page, site)
      .map((a) => `    <xhtml:link rel="alternate" hreflang="${a.hreflang}" href="${a.href}"/>`)
      .join('\n');
    return [
      '  <url>',
      `    <loc>${pageUrl(page, locale)}</loc>`,
      alts,
      `    <priority>${page.priority}</priority>`,
      '  </url>',
    ]
      .filter((line) => line !== '')
      .join('\n');
  });
  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">',
    ...entries,
    '</urlset>',
    '',
  ].join('\n');
}

export function robotsTxt(): string {
  return `User-agent: *\nAllow: /\n\nSitemap: ${SITE_ORIGIN}/sitemap.xml\n`;
}

/** Cloudflare Pages `_redirects` content. */
export function redirectsFile(): string {
  return `${REDIRECTS.map((r) => `${r.from} ${r.to} 301`).join('\n')}\n`;
}
