import { describe, expect, it } from 'vitest';
import {
  ENGLISH_FILES,
  OTHER_LOCALES,
  TRANSLATED_FILES,
  TRANSLATED_NAMESPACES,
  createT,
  englishMessages,
  formatMessage,
  isMachineDraft,
  numbered,
  ownMessages,
  type MessageKey,
} from './index.ts';

describe('site messages', () => {
  const en = englishMessages();

  it('flags every non-English locale as a machine draft and English as reviewed', () => {
    expect(isMachineDraft('en')).toBe(false);
    for (const l of OTHER_LOCALES) expect(isMachineDraft(l)).toBe(true);
  });

  /** The English keys of one file, by namespace (each English file is its namespace). */
  const englishKeys = (ns: (typeof TRANSLATED_NAMESPACES)[number]) => Object.keys(ENGLISH_FILES[ns]).filter((k) => k !== '$meta');

  it.each(OTHER_LOCALES)('%s translates every key of common, home and retailers and has no strays', (locale) => {
    for (const ns of TRANSLATED_NAMESPACES) {
      const own = Object.keys(TRANSLATED_FILES[locale][ns]).filter((k) => k !== '$meta');
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

  it('reads numbered keys in order until the first gap', () => {
    const items = numbered(createT('en'), 'en', 'home.shoppers');
    expect(items.length).toBeGreaterThanOrEqual(3);
    expect(items[0]).toBe(createT('en')('home.shoppers.1' as MessageKey));
  });

  it('never uses emoji or raw hex colours in copy', () => {
    for (const text of Object.values(en)) {
      expect(text.replace(/[©®™]/g, '')).not.toMatch(/\p{Extended_Pictographic}/u);
      expect(text).not.toMatch(/#[0-9a-fA-F]{6}\b/);
    }
  });
});
