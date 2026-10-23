// The site's message catalogues live in @todayz/i18n (messages/site/<area>.<lc>.json, owned by W1-09)
// so the shared missing-key lint (make check-i18n-messages) checks them with the apps':
//   - English files hold FormatJS descriptors: { "key": { "defaultMessage": "…", "description": "…" } }.
//   - Other locales hold { "key": { "message": "…", "review": "machine"|"reviewed", "enHash": "…" } }.
// A locale ships only when every key of the translated areas is reviewed and current (see
// isMachineDraft); otherwise production builds leave it out entirely (siteLocales).
// The site has no client i18n: text is formatted at build time. Only literals and {name}
// arguments are supported, so a catalogue can never ship half-formatted ICU.
//
// Imports are explicit (not import.meta.glob) so Node scripts (OG images, tests) load the same files.
import { createHash } from 'node:crypto';
import { isPreviewMode } from '../lib/env.ts';
import commonEn from '@todayz/i18n/messages/site/common.en.json' with { type: 'json' };
import homeEn from '@todayz/i18n/messages/site/home.en.json' with { type: 'json' };
import infoEn from '@todayz/i18n/messages/site/info.en.json' with { type: 'json' };
import legalEn from '@todayz/i18n/messages/site/legal.en.json' with { type: 'json' };
import retailersEn from '@todayz/i18n/messages/site/retailers.en.json' with { type: 'json' };
import commonHi from '@todayz/i18n/messages/site/common.hi.json' with { type: 'json' };
import homeHi from '@todayz/i18n/messages/site/home.hi.json' with { type: 'json' };
import retailersHi from '@todayz/i18n/messages/site/retailers.hi.json' with { type: 'json' };
import commonKn from '@todayz/i18n/messages/site/common.kn.json' with { type: 'json' };
import homeKn from '@todayz/i18n/messages/site/home.kn.json' with { type: 'json' };
import retailersKn from '@todayz/i18n/messages/site/retailers.kn.json' with { type: 'json' };
import commonTe from '@todayz/i18n/messages/site/common.te.json' with { type: 'json' };
import homeTe from '@todayz/i18n/messages/site/home.te.json' with { type: 'json' };
import retailersTe from '@todayz/i18n/messages/site/retailers.te.json' with { type: 'json' };
import commonTa from '@todayz/i18n/messages/site/common.ta.json' with { type: 'json' };
import homeTa from '@todayz/i18n/messages/site/home.ta.json' with { type: 'json' };
import retailersTa from '@todayz/i18n/messages/site/retailers.ta.json' with { type: 'json' };

export const LOCALES = ['en', 'hi', 'kn', 'te', 'ta'] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = 'en';
/** Locales other than English: they get the home and for-retailers pages (S-05). */
export const OTHER_LOCALES = ['hi', 'kn', 'te', 'ta'] as const satisfies readonly Locale[];

export interface LocaleInfo {
  /** BCP 47 tag for <html lang>. */
  tag: string;
  /** Tag for og:locale. */
  og: string;
  /** Name of the language in its own script (LanguagePicker rule, design/source/localization.md). */
  native: string;
}

export const LOCALE_INFO: Record<Locale, LocaleInfo> = {
  en: { tag: 'en-IN', og: 'en_IN', native: 'English' },
  hi: { tag: 'hi-IN', og: 'hi_IN', native: 'हिन्दी' },
  kn: { tag: 'kn-IN', og: 'kn_IN', native: 'ಕನ್ನಡ' },
  te: { tag: 'te-IN', og: 'te_IN', native: 'తెలుగు' },
  ta: { tag: 'ta-IN', og: 'ta_IN', native: 'தமிழ்' },
};

type Descriptor = { defaultMessage: string; description?: string };
type EnglishFile = Record<string, Descriptor>;
export type TranslationEntry = { message: string; review: string; enHash: string };
type TranslatedFile = Record<string, TranslationEntry>;

/** Fingerprint of an English message, as @todayz/i18n computes it: a different enHash means stale. */
export function enHash(message: string): string {
  return createHash('sha1').update(message).digest('hex').slice(0, 8);
}

function flattenEnglish(...files: EnglishFile[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const file of files) {
    for (const [key, value] of Object.entries(file)) {
      if (key in out) throw new Error(`site i18n: duplicate key ${key}`);
      out[key] = value.defaultMessage;
    }
  }
  return out;
}

function flattenTranslated(files: TranslatedFile[]): Record<string, TranslationEntry> {
  const out: Record<string, TranslationEntry> = {};
  for (const file of files) {
    for (const [key, value] of Object.entries(file)) {
      if (typeof value?.message !== 'string') throw new Error(`site i18n: ${key} needs a string message`);
      if (key in out) throw new Error(`site i18n: duplicate key ${key}`);
      out[key] = value;
    }
  }
  return out;
}

/** Every English file, by namespace. The translated namespaces are listed in TRANSLATED_NAMESPACES. */
export const ENGLISH_FILES = {
  common: commonEn,
  home: homeEn,
  retailers: retailersEn,
  info: infoEn,
  legal: legalEn,
} as const satisfies Record<string, EnglishFile>;

/** Namespaces translated into hi, kn, te and ta (the shared chrome, home and for-retailers pages). */
export const TRANSLATED_NAMESPACES = ['common', 'home', 'retailers'] as const;
export type TranslatedNamespace = (typeof TRANSLATED_NAMESPACES)[number];

export const TRANSLATED_FILES: Record<(typeof OTHER_LOCALES)[number], Record<TranslatedNamespace, TranslatedFile>> = {
  hi: { common: commonHi, home: homeHi, retailers: retailersHi },
  kn: { common: commonKn, home: homeKn, retailers: retailersKn },
  te: { common: commonTe, home: homeTe, retailers: retailersTe },
  ta: { common: commonTa, home: homeTa, retailers: retailersTa },
};

const en = flattenEnglish(...(Object.values(ENGLISH_FILES) as EnglishFile[]));

export type MessageKey = keyof typeof commonEn | keyof typeof homeEn | keyof typeof retailersEn | keyof typeof infoEn | keyof typeof legalEn;

const translations = new Map<Locale, Record<string, TranslationEntry>>();
for (const locale of OTHER_LOCALES) translations.set(locale, flattenTranslated(Object.values(TRANSLATED_FILES[locale])));

/** English catalogue (the reference): key to text. */
export function englishMessages(): Readonly<Record<string, string>> {
  return en;
}

/** The raw messages of one locale, without the English fallback. */
export function ownMessages(locale: Locale): Readonly<Record<string, string>> {
  if (locale === DEFAULT_LOCALE) return en;
  const entries = translations.get(locale);
  if (!entries) throw new Error(`site i18n: no catalogue for ${locale}`);
  return Object.fromEntries(Object.entries(entries).map(([k, v]) => [k, v.message]));
}

/** English keys of the translated areas: a shippable locale has a reviewed, current text for each. */
const translatedKeys = TRANSLATED_NAMESPACES.flatMap((ns) => Object.keys(ENGLISH_FILES[ns]));

/**
 * True when `entries` has, for every key in `keys`, a translation marked "reviewed" whose enHash
 * matches the English text today. A missing key, a machine draft, a stale text or any other review
 * value makes it false (fail closed).
 */
export function fullyReviewed(
  entries: Readonly<Record<string, TranslationEntry>> | undefined,
  keys: readonly string[],
  english: Readonly<Record<string, string>>,
): boolean {
  if (!entries || keys.length === 0) return false;
  return keys.every((key) => {
    const entry = entries[key];
    const text = english[key];
    return entry !== undefined && text !== undefined && entry.review === 'reviewed' && entry.enHash === enHash(text);
  });
}

/** True for every locale but English unless all its translated areas are reviewed and current. */
export function isMachineDraft(locale: Locale): boolean {
  if (locale === DEFAULT_LOCALE) return false;
  return !fullyReviewed(translations.get(locale), translatedKeys, en);
}

/** The locales a build ships: all of them in dev and e2e, English plus reviewed locales otherwise. */
export function siteLocales(mode: unknown): readonly Locale[] {
  return isPreviewMode(mode) ? LOCALES : LOCALES.filter((l) => !isMachineDraft(l));
}

export function formatMessage(template: string, values: Readonly<Record<string, string | number>> = {}, key = 'message'): string {
  return template.replace(/\{(\w+)\}/g, (_, name: string) => {
    const v = values[name];
    if (v === undefined) throw new Error(`site i18n: ${key} needs {${name}}`);
    return String(v);
  });
}

export type Translate = (key: MessageKey, values?: Readonly<Record<string, string | number>>) => string;

/** Text for a locale, falling back to English for a key that locale does not have. */
export function createT(locale: Locale): Translate {
  const own = ownMessages(locale);
  return (key, values) => {
    const text = own[key] ?? en[key];
    if (text === undefined) throw new Error(`site i18n: missing message ${key}`);
    return formatMessage(text, values, key);
  };
}

export function isLocale(value: string | undefined): value is Locale {
  return (LOCALES as readonly string[]).includes(value ?? '');
}
