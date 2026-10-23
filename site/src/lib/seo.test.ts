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
    expect(allRoutes(LOCALES)).toHaveLength(2 * 5 + 8);
    expect(allRoutes(['en'])).toHaveLength(10);
  });

  it('computes localised paths', () => {
    const home = pageById('home');
    const retailers = pageById('retailers');
    expect(pagePath(home, 'en')).toBe('/');
    expect(pagePath(home, 'hi')).toBe('/hi/');
    expect(pagePath(retailers, 'kn')).toBe('/kn/for-retailers/');
    expect(pageUrl(retailers, 'en')).toBe('https://todayz.in/for-retailers/');
  });

  it('gives every page path a leading and trailing slash (the site builds directory URLs)', () => {
    for (const { page, locale } of allRoutes(LOCALES)) expect(pagePath(page, locale)).toMatch(/^\/(?:[a-z0-9-]+\/)*$/);
  });

  it('emits hreflang alternates with x-default only for multi-language pages', () => {
    const a = alternates(pageById('home'), LOCALES);
    expect(a.map((x) => x.hreflang)).toEqual(['en', 'hi', 'kn', 'te', 'ta', 'x-default']);
    expect(a.at(-1)?.href).toBe('https://todayz.in/');
    expect(alternates(pageById('how'), LOCALES)).toEqual([]);
    // A build that ships only some locales names only those.
    expect(alternates(pageById('home'), ['en', 'kn']).map((x) => x.hreflang)).toEqual(['en', 'kn', 'x-default']);
    expect(alternates(pageById('home'), ['en'])).toEqual([]);
  });
});

describe('sitemap, robots and redirects', () => {
  it('lists every route once, with absolute canonical URLs', () => {
    const xml = sitemapXml(LOCALES);
    const locs = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]);
    expect(locs).toHaveLength(allRoutes(LOCALES).length);
    expect(new Set(locs).size).toBe(locs.length);
    for (const loc of locs) expect(loc).toMatch(/^https:\/\/todayz\.in\//);
    expect(xml).toContain('hreflang="ta" href="https://todayz.in/ta/for-retailers/"');
  });

  it('leaves locales the build does not ship out of the sitemap entirely', () => {
    const xml = sitemapXml(['en']);
    expect(xml).not.toMatch(/todayz\.in\/(?:hi|kn|te|ta)\//);
    expect(xml).not.toMatch(/hreflang=/);
    expect([...xml.matchAll(/<loc>/g)]).toHaveLength(10);
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

  it('redirects only to built pages, never to itself, and from each old URL once', () => {
    const built = new Set(allRoutes(['en']).map(({ page, locale }) => pagePath(page, locale)));
    for (const r of REDIRECTS) {
      expect(built, r.to).toContain(r.to);
      expect(r.from).not.toBe(r.to);
      expect(built.has(r.from), `${r.from} is a live page`).toBe(false);
    }
    expect(new Set(REDIRECTS.map((r) => r.from)).size).toBe(REDIRECTS.length);
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
