import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { LOCALES } from '../i18n/index.ts';
import { company } from './company.ts';
import { faqJsonLd, organizationJsonLd, softwareApplicationJsonLd, websiteJsonLd } from './jsonld.ts';
import { PAGES, alternates, allRoutes, pageById, pagePath, pageUrl } from './pages.ts';
import { REDIRECTS, redirectsFile, robotsTxt, sitemapXml } from './seo.ts';

describe('page registry', () => {
  it('has unique ids and paths', () => {
    expect(new Set(PAGES.map((p) => p.id)).size).toBe(PAGES.length);
    expect(new Set(PAGES.map((p) => p.path)).size).toBe(PAGES.length);
  });

  it('builds home and retailers in five languages and everything else in English only', () => {
    for (const p of PAGES) {
      const expected = p.id === 'home' || p.id === 'retailers' ? LOCALES : ['en'];
      expect(p.locales).toEqual(expected);
    }
    expect(allRoutes()).toHaveLength(2 * 5 + 8);
  });

  it('computes localised paths', () => {
    const home = pageById('home');
    const retailers = pageById('retailers');
    expect(pagePath(home, 'en')).toBe('/');
    expect(pagePath(home, 'hi')).toBe('/hi/');
    expect(pagePath(retailers, 'kn')).toBe('/kn/for-retailers');
    expect(pageUrl(retailers, 'en')).toBe('https://todayz.in/for-retailers');
  });

  it('emits hreflang alternates with x-default only for multi-language pages', () => {
    const a = alternates(pageById('home'));
    expect(a.map((x) => x.hreflang)).toEqual(['en', 'hi', 'kn', 'te', 'ta', 'x-default']);
    expect(a.at(-1)?.href).toBe('https://todayz.in/');
    expect(alternates(pageById('how'))).toEqual([]);
  });
});

describe('sitemap, robots and redirects', () => {
  it('lists every route once, with absolute canonical URLs', () => {
    const xml = sitemapXml();
    const locs = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]);
    expect(locs).toHaveLength(allRoutes().length);
    expect(new Set(locs).size).toBe(locs.length);
    for (const loc of locs) expect(loc).toMatch(/^https:\/\/todayz\.in\//);
    expect(xml).toContain('hreflang="ta" href="https://todayz.in/ta/for-retailers"');
  });

  it('points robots at the sitemap and allows crawling', () => {
    expect(robotsTxt()).toContain('Sitemap: https://todayz.in/sitemap.xml');
    expect(robotsTxt()).not.toMatch(/Disallow:\s*\//);
  });

  it('redirects the old landing page with a 301 and public/_redirects matches the map', () => {
    expect(REDIRECTS).toContainEqual({ from: '/todayz-landing-page.html', to: '/' });
    expect(redirectsFile()).toContain('/todayz-landing-page.html / 301');
    expect(readFileSync(new URL('../../public/_redirects', import.meta.url), 'utf8')).toBe(redirectsFile());
  });
});

describe('JSON-LD', () => {
  it('describes the organisation without inventing facts', () => {
    const org = organizationJsonLd(company);
    expect(org['@type']).toBe('Organization');
    expect(org).not.toHaveProperty('email');
    expect(org).not.toHaveProperty('sameAs');
    expect(websiteJsonLd('hi')).toMatchObject({ '@type': 'WebSite', inLanguage: 'hi-IN' });
  });

  it('describes the shop app as free to use and the FAQ as question and answer pairs', () => {
    expect(softwareApplicationJsonLd('en', 'x')).toMatchObject({ offers: { price: '0', priceCurrency: 'INR' } });
    const faq = faqJsonLd([{ q: 'Q?', a: 'A.' }]) as { mainEntity: { name: string; acceptedAnswer: { text: string } }[] };
    expect(faq.mainEntity[0]?.name).toBe('Q?');
    expect(faq.mainEntity[0]?.acceptedAnswer.text).toBe('A.');
  });
});
