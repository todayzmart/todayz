import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';
import { describe, expect, it } from 'vitest';
import { declaredVars, undefinedVars, usedVars } from '../src/css-vars.mjs';
import { pwaManifest } from '../src/vite.mjs';
import { flattenTokens, stripRemoteImports } from '../scripts/gen.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const gen = join(pkg, 'generated');
const read = (f: string) => readFileSync(join(gen, f), 'utf8');
const designTokens: unknown = JSON.parse(readFileSync(resolve(pkg, '../../design', 'source', 'tokens.json'), 'utf8'));

describe('generated output', () => {
  it('exists (run make gen first)', () => {
    for (const f of ['tokens.css', 'tokens.ts', 'tokens.json', 'bundle.css', 'fonts.css']) {
      expect(existsSync(join(gen, f)), f).toBe(true);
    }
  });

  it('defines every var that bundle.css uses', () => {
    expect(undefinedVars(read('bundle.css'), read('tokens.css'))).toEqual([]);
  });

  it('defines every var that the entry stylesheet chain uses', () => {
    const all = ['fonts.css', 'tokens.css', 'bundle.css'].map(read).join('\n');
    expect(undefinedVars(all)).toEqual([]);
  });

  it('declares one :root var per design token, light theme only', () => {
    const css = read('tokens.css');
    const vars = flattenTokens(designTokens);
    const declared = declaredVars(css);
    for (const [name] of vars) expect(declared.has(name), name).toBe(true);
    expect(css).not.toMatch(/data-theme|prefers-color-scheme|dark/);
    expect(css).toMatch(/color-scheme: light/);
  });

  it('keeps the design harness names for fonts', () => {
    const css = read('tokens.css');
    expect(css).toMatch(/--font-display: "Anek Latin"/);
    expect(css).toMatch(/--font-sans: Figtree/);
  });

  it('never points at a remote host from any CSS', () => {
    for (const f of ['tokens.css', 'bundle.css', 'fonts.css']) {
      const css = read(f);
      expect(css, f).not.toMatch(/googleapis|gstatic|https?:\/\//i);
      expect(css, f).not.toMatch(/@import/);
    }
  });

  it('self-hosts a face for every family the font tokens name', () => {
    const tokens = JSON.parse(read('tokens.json')) as Record<string, string>;
    const fonts = read('fonts.css');
    const faces = new Set([...fonts.matchAll(/font-family: '([^']+)'/g)].map((m) => m[1]));
    for (const stack of [tokens['font-display'], tokens['font-sans']]) {
      const families = (stack ?? '').split(',').map((s) => s.trim().replace(/"/g, ''));
      for (const fam of families.filter((f) => !['system-ui', 'sans-serif'].includes(f))) {
        expect(faces.has(fam), fam).toBe(true);
      }
    }
    for (const m of fonts.matchAll(/url\(\.\/fonts\/([^)]+)\)/g)) {
      expect(existsSync(join(gen, 'fonts', m[1] ?? '')), m[1]).toBe(true);
    }
  });

  it('cuts PWA icons at the declared sizes', async () => {
    const want: Record<string, number> = {
      'icon-192.png': 192,
      'icon-512.png': 512,
      'maskable-512.png': 512,
      'apple-touch-icon.png': 180,
      'favicon-32.png': 32,
      'favicon-48.png': 48,
    };
    for (const [f, size] of Object.entries(want)) {
      const meta = await sharp(join(gen, 'icons', f)).metadata();
      expect([meta.width, meta.height], f).toEqual([size, size]);
    }
    expect(readdirSync(join(gen, 'icons')).length).toBeGreaterThanOrEqual(6);
  });

  it('builds the manifest with token colours and generated icons', () => {
    const m = pwaManifest({ name: 'TodayZ', shortName: 'TodayZ', description: 'x' });
    const tokens = JSON.parse(read('tokens.json')) as Record<string, string>;
    expect(m.theme_color).toBe(tokens['bg']);
    expect(m.icons.map((i) => i.src)).toEqual(['/icons/icon-192.png', '/icons/icon-512.png', '/icons/maskable-512.png']);
  });
});

/** WCAG 2 contrast ratio of two #rrggbb colours. */
function contrast(a: string, b: string) {
  const lum = (hex: string) => {
    const [r, g, bl] = [1, 3, 5].map((i) => {
      const c = parseInt(hex.slice(i, i + 2), 16) / 255;
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (bl ?? 0);
  };
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return ((hi ?? 0) + 0.05) / ((lo ?? 0) + 0.05);
}

describe('the dark green rail (D-274)', () => {
  const tokens = () => JSON.parse(read('tokens.json')) as Record<string, string>;

  it('keeps AA (4.5:1) for every text colour on the rail fills', () => {
    const t = tokens();
    for (const bg of ['rail', 'rail-raised']) {
      for (const fg of ['on-rail', 'on-rail-2', 'rail-accent']) {
        expect(contrast(t[fg] ?? '', t[bg] ?? ''), `${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5);
      }
    }
    // The done disc: rail text on the accent fill.
    expect(contrast(t['rail'] ?? '', t['rail-accent'] ?? '')).toBeGreaterThanOrEqual(4.5);
  });
});

describe('helpers', () => {
  it('undefinedVars reports a var nobody declares', () => {
    const css = '.a { color: var(--ink); background: var(--nope, red); --local: 1px; margin: var(--local); }';
    expect(undefinedVars(css, ':root { --ink: #000; }')).toEqual(['nope']);
    expect([...usedVars(css)].sort()).toEqual(['ink', 'local', 'nope']);
  });

  it('stripRemoteImports drops a Google Fonts import and keeps the rest', () => {
    const css = "@import url('https://fonts.googleapis.com/css2?family=Figtree');\n:root { color-scheme: light; }";
    expect(stripRemoteImports(css)).toBe(':root { color-scheme: light; }');
  });
});
