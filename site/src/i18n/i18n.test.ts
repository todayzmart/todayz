import { describe, expect, it } from 'vitest';
import {
  OTHER_LOCALES,
  TRANSLATED_FILES,
  TRANSLATED_NAMESPACES,
  createT,
  englishMessages,
  formatMessage,
  isMachineDraft,
  numbered,
  type MessageKey,
} from './index.ts';

describe('site messages', () => {
  const en = englishMessages();

  it('flags every non-English locale as a machine draft and English as reviewed', () => {
    expect(isMachineDraft('en')).toBe(false);
    for (const l of OTHER_LOCALES) expect(isMachineDraft(l)).toBe(true);
  });

  it.each(OTHER_LOCALES)('%s covers every English key of the translated namespaces and has no strays', (locale) => {
    for (const ns of TRANSLATED_NAMESPACES) {
      const file = TRANSLATED_FILES[locale][ns];
      const own = Object.keys(file).filter((k) => k !== '$meta');
      const english = Object.keys(en).filter((k) => k.split('.')[0] === (ns === 'common' ? k.split('.')[0] : ns));
      // Every key the locale defines exists in English (no strays).
      for (const k of own) expect(en, `${locale}:${k}`).toHaveProperty(k);
      // Every English key of this namespace is translated.
      if (ns !== 'common') for (const k of english) expect(own, `${locale} misses ${k}`).toContain(k);
    }
  });

  it('keeps arguments identical between English and each translation', () => {
    const args = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
    for (const locale of OTHER_LOCALES) {
      const t = createT(locale);
      for (const key of Object.keys(en)) {
        const translated = (() => {
          try {
            return t(key as MessageKey, Object.fromEntries(args(en[key] ?? '').map((a) => [a, `<${a}>`])));
          } catch {
            return null;
          }
        })();
        expect(translated, `${locale}:${key}`).not.toBeNull();
      }
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
