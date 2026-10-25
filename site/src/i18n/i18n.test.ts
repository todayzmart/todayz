import { describe, expect, it } from 'vitest';
import {
  ENGLISH_FILES,
  OTHER_LOCALES,
  TRANSLATED_FILES,
  TRANSLATED_NAMESPACES,
  createT,
  englishMessages,
  formatMessage,
  enHash,
  fullyReviewed,
  isMachineDraft,
  siteLocales,
  ownMessages,
} from './index.ts';
import { isPreviewMode } from '../lib/env.ts';

describe('site messages', () => {
  const en = englishMessages();

  it('treats English and every approved locale as reviewed (W4-05: the founder approved hi, kn, te, ta)', () => {
    expect(isMachineDraft('en')).toBe(false);
    for (const l of OTHER_LOCALES) expect(isMachineDraft(l)).toBe(false);
  });

  /** The English keys of one file, by namespace (each English file is its namespace). */
  const englishKeys = (ns: (typeof TRANSLATED_NAMESPACES)[number]) => Object.keys(ENGLISH_FILES[ns]);

  it.each(OTHER_LOCALES)('%s translates every key of common, home and retailers and has no strays', (locale) => {
    for (const ns of TRANSLATED_NAMESPACES) {
      const own = Object.keys(TRANSLATED_FILES[locale][ns]);
      const english = englishKeys(ns);
      expect(english.length, ns).toBeGreaterThan(0);
      expect(own.filter((k) => !english.includes(k)), `${locale}/${ns} keys English does not have`).toEqual([]);
      expect(english.filter((k) => !own.includes(k)), `${locale}/${ns} keys not translated`).toEqual([]);
    }
  });

  it.each(OTHER_LOCALES)('%s uses exactly the arguments of the English text, key by key', (locale) => {
    const args = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
    const own = ownMessages(locale);
    const mismatches = Object.keys(own)
      .filter((k) => JSON.stringify(args(own[k] ?? '')) !== JSON.stringify(args(en[k] ?? '')))
      .map((k) => `${k}: ${JSON.stringify(args(own[k] ?? ''))} vs English ${JSON.stringify(args(en[k] ?? ''))}`);
    expect(mismatches).toEqual([]);
  });

  it.each(OTHER_LOCALES)('%s keeps no braces other than {name} arguments', (locale) => {
    for (const [k, text] of Object.entries(ownMessages(locale))) {
      expect(text.replace(/\{\w+\}/g, ''), `${locale}:${k}`).not.toMatch(/[{}]/);
    }
  });

  it('falls back to English for a key a locale does not define', () => {
    expect(createT('hi')('legal.privacy.title')).toBe(createT('en')('legal.privacy.title'));
  });

  it('formats literals and {name} arguments only', () => {
    expect(formatMessage('{distance} away', { distance: '500 m' })).toBe('500 m away');
    expect(() => formatMessage('{n, plural, one {a} other {b}}', { n: 1 })).toThrow();
  });

  describe('machine drafts fail closed', () => {
    const english = { 'a.one': 'One', 'a.two': 'Two' };
    const keys = Object.keys(english);
    const ok = (k: keyof typeof english) => ({ message: 'x', review: 'reviewed', enHash: enHash(english[k]) });

    it('ships a locale only when every key is reviewed and current', () => {
      expect(fullyReviewed({ 'a.one': ok('a.one'), 'a.two': ok('a.two') }, keys, english)).toBe(true);
    });
    it.each([
      ['a missing key', { 'a.one': ok('a.one') }],
      ['a machine draft', { 'a.one': ok('a.one'), 'a.two': { ...ok('a.two'), review: 'machine' } }],
      ['an unknown review value', { 'a.one': ok('a.one'), 'a.two': { ...ok('a.two'), review: 'Reviewed' } }],
      ['a stale text (English changed)', { 'a.one': ok('a.one'), 'a.two': { ...ok('a.two'), enHash: enHash('Old two') } }],
    ])('treats %s as a draft', (_, entries) => {
      expect(fullyReviewed(entries, keys, english)).toBe(false);
    });
    it('treats a locale with no file or no keys as a draft', () => {
      expect(fullyReviewed(undefined, keys, english)).toBe(false);
      expect(fullyReviewed({}, [], english)).toBe(false);
    });
  });

  it('shows drafts only in development and e2e builds', () => {
    expect(isPreviewMode('development')).toBe(true);
    expect(isPreviewMode('e2e')).toBe(true);
    for (const mode of ['production', 'staging', 'preview', '', undefined]) expect(isPreviewMode(mode)).toBe(false);
    expect(siteLocales('e2e')).toEqual(['en', 'hi', 'kn', 'te', 'ta']);
    // Every locale is reviewed since W4-05, so production ships them all.
    expect(siteLocales('production')).toEqual(['en', 'hi', 'kn', 'te', 'ta']);
    expect(siteLocales('staging')).toEqual(['en', 'hi', 'kn', 'te', 'ta']);
  });

  it('never uses emoji or raw hex colours in copy', () => {
    for (const text of Object.values(en)) {
      expect(text.replace(/[©®™]/g, '')).not.toMatch(/\p{Extended_Pictographic}/u);
      expect(text).not.toMatch(/#[0-9a-fA-F]{6}\b/);
    }
  });
});
