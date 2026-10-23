import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  FONT_FAMILIES,
  LANGUAGE_STORAGE_KEY,
  LOCALES,
  NATIVE_NAMES,
  PSEUDO_LOCALE,
  SCRIPT_OF,
  applyLocale,
  browserStore,
  detectLocale,
  fontFamiliesFor,
  fontLoadRequests,
  hasChosenLocale,
  htmlLang,
  loadFonts,
  loadLocale,
  localeFromSearch,
  matchLanguageTag,
  matchLanguages,
  saveLocale,
  scriptOf,
  type KeyValueStore,
} from '../src/index';
import { TRANSLATED_LOCALES } from '../scripts/catalogue.mjs';

function memoryStore(initial: Record<string, string> = {}): KeyValueStore & { data: Map<string, string> } {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
  };
}

const brokenStore: KeyValueStore = {
  getItem() {
    throw new Error('denied');
  },
  setItem() {
    throw new Error('denied');
  },
  removeItem() {
    throw new Error('denied');
  },
};

describe('SC-I18N-04 languages', () => {
  it('ships exactly en, hi, kn, te, ta, with the catalogue script in step', () => {
    expect([...LOCALES]).toEqual(['en', 'hi', 'kn', 'te', 'ta']);
    expect(TRANSLATED_LOCALES).toEqual(LOCALES.filter((l) => l !== 'en'));
    expect(NATIVE_NAMES).toEqual({ en: 'English', hi: 'हिन्दी', kn: 'ಕನ್ನಡ', te: 'తెలుగు', ta: 'தமிழ்' });
  });

  it.each([
    ['hi', 'hi'],
    ['hi-IN', 'hi'],
    ['KN-in', 'kn'],
    ['te_IN', 'te'],
    ['ta-LK', 'ta'],
    ['en-US', 'en'],
    ['fr-FR', undefined],
    ['', undefined],
    ['hi-Latn', 'en'], // Hindi in Latin letters is not Devanagari: English
    ['ta-Latn-IN', 'en'],
    ['kn_latn', 'en'],
    ['te-Telu', 'te'],
    ['fr-Latn', undefined],
  ])('matches tag %s -> %s', (tag, want) => {
    expect(matchLanguageTag(tag)).toBe(want);
  });

  it('a Latin-script Indic browser language does not pick the Indic catalogue', () => {
    expect(detectLocale({ languages: ['hi-Latn'] })).toBe('en');
  });

  it('picks the first supported browser language', () => {
    expect(matchLanguages(['fr-FR', 'kn-IN', 'en'])).toBe('kn');
    expect(matchLanguages(['fr', 'de'])).toBeUndefined();
  });

  it.each([
    ['en', 'en-IN'],
    ['hi', 'hi'],
    ['kn', 'kn'],
    ['te', 'te'],
    ['ta', 'ta'],
    [PSEUDO_LOCALE, 'en-XA'],
  ] as const)('html lang for %s is %s', (locale, want) => {
    expect(htmlLang(locale)).toBe(want);
  });
});

describe('SC-I18N-04 detection and persistence', () => {
  it('falls back to English with nothing to go on', () => {
    expect(detectLocale()).toBe('en');
    expect(detectLocale({ languages: ['fr'] })).toBe('en');
  });

  it('uses the browser language', () => {
    expect(detectLocale({ languages: ['ta-IN', 'en'] })).toBe('ta');
  });

  it('prefers the stored choice, then the URL, then the browser', () => {
    const store = memoryStore({ [LANGUAGE_STORAGE_KEY]: 'te' });
    expect(detectLocale({ store, search: '?locale=hi', languages: ['kn'] })).toBe('te');
    expect(detectLocale({ search: '?locale=hi', languages: ['kn'] })).toBe('hi');
    expect(detectLocale({ store: memoryStore(), languages: ['kn'] })).toBe('kn');
  });

  it('round-trips a choice for each language', () => {
    for (const lc of LOCALES) {
      const store = memoryStore();
      expect(hasChosenLocale(store)).toBe(false);
      expect(saveLocale(store, lc)).toBe(true);
      expect(loadLocale(store)).toBe(lc);
      expect(hasChosenLocale(store)).toBe(true);
      expect(detectLocale({ store, languages: ['en'] })).toBe(lc);
    }
  });

  it('ignores garbage in storage', () => {
    expect(loadLocale(memoryStore({ [LANGUAGE_STORAGE_KEY]: 'klingon' }))).toBeUndefined();
    expect(detectLocale({ store: memoryStore({ [LANGUAGE_STORAGE_KEY]: 'klingon' }), languages: ['hi'] })).toBe('hi');
  });

  it('survives unavailable storage (private mode)', () => {
    expect(loadLocale(brokenStore)).toBeUndefined();
    expect(saveLocale(brokenStore, 'hi')).toBe(false);
    expect(saveLocale(undefined, 'hi')).toBe(false);
    expect(detectLocale({ store: brokenStore, languages: ['kn'] })).toBe('kn');
  });

  it('browserStore returns localStorage, and undefined when reading it throws or it is missing', () => {
    const g = globalThis as { localStorage?: unknown };
    const had = Object.getOwnPropertyDescriptor(g, 'localStorage');
    try {
      const fake = memoryStore();
      Object.defineProperty(g, 'localStorage', { value: fake, configurable: true });
      expect(browserStore()).toBe(fake);
      Object.defineProperty(g, 'localStorage', {
        get() {
          throw new DOMException('denied', 'SecurityError');
        },
        configurable: true,
      });
      expect(browserStore()).toBeUndefined();
      expect(detectLocale({ store: browserStore(), languages: ['kn'] })).toBe('kn');
      delete g.localStorage;
      expect(browserStore()).toBeUndefined();
    } finally {
      if (had) Object.defineProperty(g, 'localStorage', had);
      else delete g.localStorage;
    }
  });

  it('?locale=pseudo works only when allowed, and beats a stored choice', () => {
    const store = memoryStore({ [LANGUAGE_STORAGE_KEY]: 'hi' });
    expect(localeFromSearch('?locale=pseudo')).toBeUndefined();
    expect(detectLocale({ store, search: '?locale=pseudo' })).toBe('hi');
    expect(detectLocale({ store, search: '?locale=en-XA', allowPseudo: true })).toBe(PSEUDO_LOCALE);
    expect(store.data.get(LANGUAGE_STORAGE_KEY)).toBe('hi'); // never overwritten by a dev switch
  });
});

describe('SC-I18N-06 fonts per script', () => {
  it('maps each language to its script', () => {
    expect(SCRIPT_OF).toEqual({ en: 'latin', hi: 'devanagari', kn: 'kannada', te: 'telugu', ta: 'tamil' });
    expect(scriptOf(PSEUDO_LOCALE)).toBe('latin');
  });

  it('names only families that the design tokens declare', () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const tokens = JSON.parse(readFileSync(join(resolve(here, '..', '..', '..'), 'design', 'source', 'tokens.json'), 'utf8')) as {
      type: { families: { display: string; sans: string } };
    };
    for (const f of Object.values(FONT_FAMILIES)) {
      expect(tokens.type.families.display, f.display).toContain(f.display);
      expect(tokens.type.families.sans, f.sans).toContain(f.sans);
    }
  });

  it('puts the script first, Latin behind it, and Anek with Noto Sans for Indic scripts', () => {
    expect(fontFamiliesFor('en')).toEqual(['Anek Latin', 'Figtree']);
    expect(fontFamiliesFor('kn')).toEqual(['Anek Kannada', 'Noto Sans Kannada', 'Anek Latin', 'Figtree']);
    for (const lc of ['hi', 'te', 'ta'] as const) expect(fontFamiliesFor(lc)[1]).toMatch(/^Noto Sans /);
  });

  it('loads the right faces with a script sample', async () => {
    const asked: [string, string | undefined][] = [];
    await loadFonts('te', { load: (f, t) => (asked.push([f, t]), Promise.resolve([])) });
    expect(asked.map((a) => a[0])).toEqual(['400 16px "Noto Sans Telugu"', '700 16px "Anek Telugu"', '400 16px "Figtree"']);
    expect(asked[0]?.[1]).toBe('అఆక');
    expect(fontLoadRequests('en')).toHaveLength(2);
  });

  it('never rejects when a font fails', async () => {
    await expect(loadFonts('hi', { load: () => Promise.reject(new Error('404')) })).resolves.toBeUndefined();
    await expect(loadFonts('hi', undefined)).resolves.toBeUndefined();
  });

  it('applyLocale sets <html lang>', async () => {
    const doc = { documentElement: { lang: '' } } as unknown as Document;
    await applyLocale('kn', doc);
    expect(doc.documentElement.lang).toBe('kn');
    await applyLocale('en', doc);
    expect(doc.documentElement.lang).toBe('en-IN');
  });
});
