// Site-local message catalogues (W1-09). The shape follows shared/i18n/messages so the files can
// move there when the site switches to @todayz/i18n:
//   - English files hold FormatJS descriptors: { "key": { "defaultMessage": "…", "description": "…" } }.
//   - Other locales hold flat strings plus a "$meta" block. While "$meta.status" is "machine-draft"
//     the text was translated by a machine and waits for a human reviewer.
// The site has no client i18n: text is formatted at build time. Only literals and {name}
// arguments are supported, so a catalogue can never ship half-formatted ICU.
//
// Imports are explicit (not import.meta.glob) so Node scripts (OG images, tests) load the same files.
import commonEn from './messages/common.en.json' with { type: 'json' };
import homeEn from './messages/home.en.json' with { type: 'json' };
import infoEn from './messages/info.en.json' with { type: 'json' };
import legalEn from './messages/legal.en.json' with { type: 'json' };
import retailersEn from './messages/retailers.en.json' with { type: 'json' };
import commonHi from './messages/common.hi.json' with { type: 'json' };
import homeHi from './messages/home.hi.json' with { type: 'json' };
import retailersHi from './messages/retailers.hi.json' with { type: 'json' };
import commonKn from './messages/common.kn.json' with { type: 'json' };
import homeKn from './messages/home.kn.json' with { type: 'json' };
import retailersKn from './messages/retailers.kn.json' with { type: 'json' };
import commonTe from './messages/common.te.json' with { type: 'json' };
import homeTe from './messages/home.te.json' with { type: 'json' };
import retailersTe from './messages/retailers.te.json' with { type: 'json' };
import commonTa from './messages/common.ta.json' with { type: 'json' };
import homeTa from './messages/home.ta.json' with { type: 'json' };
import retailersTa from './messages/retailers.ta.json' with { type: 'json' };

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
type TranslatedFile = Record<string, string | { status: string }>;

export interface Meta {
  status: 'machine-draft' | 'reviewed';
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

function flattenTranslated(files: TranslatedFile[]): { messages: Record<string, string>; meta: Meta } {
  const messages: Record<string, string> = {};
  let status: Meta['status'] = 'reviewed';
  for (const file of files) {
    for (const [key, value] of Object.entries(file)) {
      if (key === '$meta') {
        if (typeof value === 'object' && value.status !== 'reviewed') status = 'machine-draft';
        continue;
      }
      if (typeof value !== 'string') throw new Error(`site i18n: ${key} must be a string`);
      if (key in messages) throw new Error(`site i18n: duplicate key ${key}`);
      messages[key] = value;
    }
  }
  return { messages, meta: { status } };
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

const catalogues = new Map<Locale, { messages: Record<string, string>; meta: Meta }>();
function catalogue(locale: Locale): { messages: Record<string, string>; meta: Meta } {
  const found = catalogues.get(locale);
  if (!found) throw new Error(`site i18n: no catalogue for ${locale}`);
  return found;
}
catalogues.set('en', { messages: en, meta: { status: 'reviewed' } });
for (const locale of OTHER_LOCALES) {
  catalogues.set(locale, flattenTranslated(Object.values(TRANSLATED_FILES[locale])));
}

/** English catalogue (the reference): key to text. */
export function englishMessages(): Readonly<Record<string, string>> {
  return en;
}

/** The raw messages of one locale, without the English fallback. */
export function ownMessages(locale: Locale): Readonly<Record<string, string>> {
  return catalogue(locale).messages;
}

/** True while the locale's text is a machine translation nobody has reviewed. */
export function isMachineDraft(locale: Locale): boolean {
  return catalogue(locale).meta.status === 'machine-draft';
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

/** Numbered keys `${prefix}.1`, `${prefix}.2`, … until the first gap, in order. */
export function numbered(t: Translate, locale: Locale, prefix: string): string[] {
  const source = locale === 'en' ? en : { ...en, ...ownMessages(locale) };
  const out: string[] = [];
  for (let i = 1; `${prefix}.${i}` in source; i += 1) out.push(t(`${prefix}.${i}` as MessageKey));
  return out;
}

export function isLocale(value: string | undefined): value is Locale {
  return (LOCALES as readonly string[]).includes(value ?? '');
}
