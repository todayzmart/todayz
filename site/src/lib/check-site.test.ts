// The link checker and HTML validator (scripts/check-site.mjs) must bite on a broken site.
import { mkdirSync, mkdtempSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { checkLinks, scan, validateHtml } from '../../scripts/check-site.mjs';
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
    for (const { page, locale } of allRoutes()) {
      const name = `${page.id}${locale === 'en' ? '' : `-${locale}`}.png`;
      expect(existsSync(new URL(`../../public/og/${name}`, import.meta.url)), name).toBe(true);
    }
  });
});
