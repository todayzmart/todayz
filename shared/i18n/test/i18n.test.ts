import { TYPE, parse, type MessageFormatElement } from '@formatjs/icu-messageformat-parser';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { compile, loadApp } from '../scripts/gen.mjs';
import { htmlLang, pseudoAllowed, resolveLanguage, resolveLocale } from '../src/index';
import { PSEUDO_GROWTH, accent, pseudoAst } from '../src/pseudo.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const apps = readdirSync(join(pkg, 'messages'));

/** Renders an AST to plain text with simple argument values, for length checks. */
function text(ast: MessageFormatElement[], values: Record<string, string> = {}): string {
  return ast
    .map((el) => {
      if (el.type === TYPE.literal) return el.value;
      if (el.type === TYPE.argument) return values[el.value] ?? 'xxxx';
      if (el.type === TYPE.tag) return text(el.children, values);
      return 'xxxx';
    })
    .join('');
}

/** Product and brand names that keep their capitals mid-sentence. */
const PROPER_NOUNS = new Set([
  'TodayZ', 'WhatsApp', 'Google', 'India', 'Indian', 'English', 'JavaScript', 'Cloudflare', 'Turnstile', 'I',
  'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday',
  'January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December',
]);
/** Multi-word names (a sample shop in the site's mock, a statutory body in the privacy policy). */
const PROPER_PHRASES = ['Sharma General Store', 'Data Protection Board'];

/**
 * Words that break sentence case: a capitalised word that is not the first word of a sentence,
 * not an acronym (UPI, KYC) and not a proper noun. Placeholders and quoted UI labels
 * (“Open now”, which keep the label's own case) are ignored.
 */
function titleCaseWords(msg: string): string[] {
  const out: string[] = [];
  const bare = msg.replace(/\{[^}]*\}/g, 'x').replace(/“[^”]*”/g, 'x');
  const text = PROPER_PHRASES.reduce((m, name) => m.replaceAll(name, 'x'), bare);
  for (const sentence of text.split(/(?<=[.?:])\s+/)) {
    for (const word of sentence.split(/\s+/).slice(1)) {
      const w = word.replace(/^[^\p{L}]+|[^\p{L}\d]+$/gu, '').replace(/['’]s$/u, '');
      if (!/^\p{Lu}/u.test(w) || PROPER_NOUNS.has(w) || /^[\p{Lu}\d]{2,}$/u.test(w)) continue;
      out.push(w);
    }
  }
  return out;
}

describe('resolveLocale', () => {
  it.each([
    ['?locale=pseudo', 'en-XA'],
    ['?locale=en-XA', 'en-XA'],
    ['?locale=EN-xa', 'en-XA'],
    ['?locale=hi', 'en'], // pseudo-only: a real language in the URL is not this function's job
    ['?locale=fr', 'en'],
    ['', 'en'],
  ])('%s -> %s', (search, want) => {
    expect(resolveLocale(search, { allowPseudo: true })).toBe(want);
  });

  it('in production mode ?locale=hi leaves resolveLocale at en', () => {
    expect(resolveLocale('?locale=hi', { allowPseudo: pseudoAllowed('production') })).toBe('en');
  });

  it.each([
    ['?locale=hi', 'hi'],
    ['?locale=ta', 'ta'],
    ['?locale=pseudo', 'en-XA'],
    ['?locale=fr', 'en'],
    ['', 'en'],
  ])('resolveLanguage %s -> %s', (search, want) => {
    expect(resolveLanguage(search, { allowPseudo: true })).toBe(want);
  });

  it('resolveLanguage ignores pseudo unless allowed', () => {
    expect(resolveLanguage('?locale=pseudo')).toBe('en');
  });

  it('ignores ?locale=pseudo unless allowed (production builds)', () => {
    expect(resolveLocale('?locale=pseudo')).toBe('en');
    expect(resolveLocale('?locale=pseudo', { allowPseudo: pseudoAllowed('production') })).toBe('en');
    expect(resolveLocale('?locale=pseudo', { allowPseudo: pseudoAllowed('e2e') })).toBe('en-XA');
    expect(pseudoAllowed('development')).toBe(true);
    expect(pseudoAllowed('test')).toBe(true);
  });

  it('sets html lang', () => {
    expect(htmlLang('en')).toBe('en-IN');
    expect(htmlLang('en-XA')).toBe('en-XA');
  });
});

describe('pseudo-locale', () => {
  it('accents letters and leaves digits and punctuation', () => {
    expect(accent('Orders 12, ok.')).toBe('Öŕðéŕš 12, öķ.');
  });

  it('is bracketed and at least 40% longer', () => {
    for (const msg of ['Home', 'Sell', 'Skip to content', 'Find what local shops have in stock, book it, and pick it up.']) {
      const out = text(pseudoAst(parse(msg)));
      expect(out.startsWith('[') && out.endsWith(']'), out).toBe(true);
      expect(out.length, out).toBeGreaterThanOrEqual(Math.ceil(msg.length * PSEUDO_GROWTH));
    }
  });

  it('keeps placeholders, plurals and tags intact', () => {
    const ast = pseudoAst(parse('{count, plural, one {# shop} other {# shops}} near <b>{area}</b>'));
    const plural = ast.find((e) => e.type === TYPE.plural);
    expect(plural && plural.type === TYPE.plural && Object.keys(plural.options)).toEqual(['one', 'other']);
    const tag = ast.find((e) => e.type === TYPE.tag);
    expect(tag && tag.type === TYPE.tag && tag.children[0]).toMatchObject({ type: TYPE.argument, value: 'area' });
  });
});

describe('catalogues', () => {
  it.each(apps)('%s compiles in en and en-XA with the same keys', async (app) => {
    const messages = await loadApp(app);
    const en = compile(messages);
    const xa = compile(messages, { pseudo: true });
    expect(Object.keys(en).length).toBeGreaterThan(0);
    expect(Object.keys(xa)).toEqual(Object.keys(en));
  });

  it.each(apps)('%s copy is sentence case with no exclamation marks or emoji', async (app) => {
    const messages = await loadApp(app);
    for (const [key, msg] of Object.entries(messages)) {
      expect(msg, key).not.toMatch(/!/);
      expect(msg, key).not.toMatch(/\p{Emoji_Presentation}/u);
      expect(titleCaseWords(msg), key).toEqual([]);
    }
  });

  it('flags title case and allows proper nouns and acronyms', () => {
    expect(titleCaseWords('Go To Orders')).toEqual(['To', 'Orders']);
    expect(titleCaseWords('Your shop on TodayZ. Pay by UPI or card.')).toEqual([]);
    expect(titleCaseWords('{count} items in {storeName}')).toEqual([]);
  });

  it('generated output exists (run make gen first)', () => {
    for (const app of apps) {
      expect(existsSync(join(pkg, 'compiled', app, 'en.json')), app).toBe(true);
      const xa = JSON.parse(readFileSync(join(pkg, 'compiled', app, 'en-XA.json'), 'utf8')) as Record<string, unknown>;
      expect(Object.keys(xa).length).toBeGreaterThan(0);
    }
  });
});
