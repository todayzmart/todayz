// The link checker and HTML validator (scripts/check-site.mjs) must bite on a broken site.
import { mkdirSync, mkdtempSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { checkLinks, internalPath, missingSlash, scan, sitemapSlashProblems, slashProblems, validateHtml } from '../../scripts/check-site.mjs';
import { LOCALES } from '../i18n/index.ts';
import { allRoutes } from './pages.ts';

const DOC = (body: string) =>
  `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>T</title></head><body>${body}</body></html>`;

function site(files: Record<string, string>): string {
  const dist = mkdtempSync(join(tmpdir(), 'tz-site-'));
  for (const [rel, html] of Object.entries(files)) {
    const file = join(dist, rel);
    mkdirSync(join(file, '..'), { recursive: true });
    writeFileSync(file, html);
  }
  return dist;
}

describe('link checker', () => {
  it('passes a site whose links, fragments and canonical URLs all resolve', () => {
    const dist = site({
      'index.html': DOC('<a href="/about">a</a><a href="#top" id="top">t</a><a href="https://todayz.in/about#story">s</a>'),
      'about/index.html': DOC('<h1 id="story">x</h1><a href="/">home</a>'),
    });
    expect(checkLinks(dist)).toEqual([]);
  });

  it('reports a missing page and a missing fragment', () => {
    const dist = site({
      'index.html': DOC('<a href="/nope">x</a><a href="/about#gone">y</a>'),
      'about/index.html': DOC('<h1>x</h1>'),
    });
    expect(checkLinks(dist)).toEqual(['index.html: broken link /nope', 'index.html: missing #gone in /about#gone']);
  });

  it('ignores other origins and mailto links', () => {
    const dist = site({ 'index.html': DOC('<a href="https://app.todayz.in/">x</a><a href="mailto:a@b.in">y</a>') });
    expect(checkLinks(dist)).toEqual([]);
  });

  it('reads links and ids from markup', () => {
    const { links, ids } = scan('<a href="/a" id="x"><img src="/i.png"></a>');
    expect(links.map((l) => l.url)).toEqual(['/a', '/i.png']);
    expect(ids.has('x')).toBe(true);
  });
});

describe('trailing slashes', () => {
  it('reads the path of internal URLs only', () => {
    expect(internalPath('/about/#story')).toBe('/about/');
    expect(internalPath('https://todayz.in')).toBe('/');
    expect(internalPath('https://todayz.in/hi/?x=1')).toBe('/hi/');
    expect(internalPath('https://todayz.in.evil.example/a')).toBeNull();
    expect(internalPath('https://app.todayz.in/a')).toBeNull();
    expect(internalPath('#main')).toBeNull();
    expect(internalPath('mailto:a@b.in')).toBeNull();
  });

  it('wants a slash on page paths, not on files', () => {
    expect(missingSlash('/about')).toBe(true);
    expect(missingSlash('/legal/privacy')).toBe(true);
    expect(missingSlash('/about/')).toBe(false);
    expect(missingSlash('/')).toBe(false);
    expect(missingSlash('/sitemap.xml')).toBe(false);
    expect(missingSlash('/_astro/a.B1c2.css')).toBe(false);
  });

  it('reports links, canonical, hreflang and og:url without the slash', () => {
    const html = DOC(
      '<a href="/about">a</a><a href="/about/">ok</a><a href="/pricing#plan">p</a><a href="https://app.todayz.in/x">app</a>',
    ).replace(
      '<title>T</title>',
      '<title>T</title><link rel="canonical" href="https://todayz.in/contact"><link rel="alternate" hreflang="hi" href="https://todayz.in/hi/"><meta property="og:url" content="https://todayz.in/legal/privacy">',
    );
    expect(slashProblems('x.html', html)).toEqual([
      'x.html: page URL without trailing slash https://todayz.in/contact',
      'x.html: page URL without trailing slash /about',
      'x.html: page URL without trailing slash /pricing#plan',
      'x.html: page URL without trailing slash https://todayz.in/legal/privacy',
    ]);
  });

  it('reports sitemap URLs without the slash or on another origin', () => {
    const xml =
      '<url><loc>https://todayz.in/about/</loc><xhtml:link rel="alternate" hreflang="hi" href="https://todayz.in/hi/for-retailers"/></url><url><loc>https://example.com/</loc></url>';
    expect(sitemapSlashProblems(xml)).toEqual([
      'sitemap.xml: URL without trailing slash or off-site https://todayz.in/hi/for-retailers',
      'sitemap.xml: URL without trailing slash or off-site https://example.com/',
    ]);
  });
});

describe('HTML validator', () => {
  it('accepts a valid document and rejects an img without alt and a duplicate id', async () => {
    expect(await validateHtml('ok.html', DOC('<main><h1>Hi</h1></main>'))).toEqual([]);
    const bad = await validateHtml('bad.html', DOC('<img src="/a.png"><p id="x"></p><p id="x"></p>'));
    expect(bad.join('\n')).toMatch(/alt/);
    expect(bad.join('\n')).toMatch(/no-dup-id/);
  });
});

describe('Open Graph images', () => {
  it('exist in public/og for every page and language', () => {
    for (const { page, locale } of allRoutes(LOCALES)) {
      const name = `${page.id}${locale === 'en' ? '' : `-${locale}`}.png`;
      expect(existsSync(new URL(`../../public/og/${name}`, import.meta.url)), name).toBe(true);
    }
  });
});
