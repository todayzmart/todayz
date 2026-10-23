// Language detection and persistence for en, hi, kn, te, ta.
// Order: the language the person chose (stored) > ?locale= in the URL > the browser's language list
// > English. Only a choice made in the app is ever stored; detection alone is not a choice, so the
// onboarding language screen (C-01) can still ask.
import { DEFAULT_LOCALE, PSEUDO_LOCALE, isLocale, type Locale, type MessageLocale } from './locales';

export const LANGUAGE_STORAGE_KEY = 'todayz.lang';

/** The slice of Storage that detection needs; localStorage fits, tests pass a Map-backed fake. */
export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** 'hi-IN' -> 'hi', 'kn' -> 'kn', 'fr' -> undefined. Underscores and case are tolerated. */
export function matchLanguageTag(tag: string): Locale | undefined {
  const lang = tag.trim().toLowerCase().replace(/_/g, '-').split('-')[0];
  return isLocale(lang) ? lang : undefined;
}

/** First supported language in a navigator.languages style list. */
export function matchLanguages(tags: readonly string[]): Locale | undefined {
  for (const tag of tags) {
    const m = matchLanguageTag(tag);
    if (m) return m;
  }
  return undefined;
}

/**
 * `?locale=hi` picks a language; `?locale=pseudo` or `en-XA` picks the pseudo-locale, but only when
 * `allowPseudo` is set (dev, tests and the e2e build; production ignores it). Anything else: undefined.
 */
export function localeFromSearch(search: string, { allowPseudo = false }: { allowPseudo?: boolean } = {}): MessageLocale | undefined {
  const v = new URLSearchParams(search).get('locale')?.toLowerCase();
  if (!v) return undefined;
  if (v === 'pseudo' || v === 'en-xa') return allowPseudo ? PSEUDO_LOCALE : undefined;
  return matchLanguageTag(v);
}

/** Reads the stored choice; a missing, invalid or unreadable store (private mode) gives undefined. */
export function loadLocale(store: KeyValueStore | undefined): Locale | undefined {
  try {
    const v = store?.getItem(LANGUAGE_STORAGE_KEY);
    return isLocale(v) ? v : undefined;
  } catch {
    return undefined;
  }
}

/** Remembers the person's choice. Returns false when storage is unavailable (the choice then lasts one session). */
export function saveLocale(store: KeyValueStore | undefined, locale: Locale): boolean {
  try {
    store?.setItem(LANGUAGE_STORAGE_KEY, locale);
    return store !== undefined;
  } catch {
    return false;
  }
}

export interface DetectInput {
  store?: KeyValueStore;
  /** location.search */
  search?: string;
  /** navigator.languages */
  languages?: readonly string[];
  allowPseudo?: boolean;
}

/** The language to start in. */
export function detectLocale({ store, search = '', languages = [], allowPseudo = false }: DetectInput = {}): MessageLocale {
  const fromUrl = localeFromSearch(search, { allowPseudo });
  if (fromUrl === PSEUDO_LOCALE) return fromUrl; // an explicit dev switch beats everything, and is never stored
  return loadLocale(store) ?? fromUrl ?? matchLanguages(languages) ?? DEFAULT_LOCALE;
}

/** Whether the person has already chosen a language (skip the C-01 language screen if so). */
export function hasChosenLocale(store: KeyValueStore | undefined): boolean {
  return loadLocale(store) !== undefined;
}
