// The five launch languages (01-product-rules.md, localization.md) and the en-XA pseudo-locale.

/** UI languages people can choose. English is the default and the fallback for every missing key. */
export const LOCALES = ['en', 'hi', 'kn', 'te', 'ta'] as const;
export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = 'en';

/** The pseudo-locale: English underneath, accented and 40% longer. Never offered to users. */
export const PSEUDO_LOCALE = 'en-XA' as const;

/** Message locales: the five languages plus the pseudo-locale. */
export type MessageLocale = Locale | typeof PSEUDO_LOCALE;

/** Each language written in its own script, for LanguagePicker. */
export const NATIVE_NAMES: Record<Locale, string> = {
  en: 'English',
  hi: 'हिन्दी',
  kn: 'ಕನ್ನಡ',
  te: 'తెలుగు',
  ta: 'தமிழ்',
};

export type Script = 'latin' | 'devanagari' | 'kannada' | 'telugu' | 'tamil';

export const SCRIPT_OF: Record<Locale, Script> = {
  en: 'latin',
  hi: 'devanagari',
  kn: 'kannada',
  te: 'telugu',
  ta: 'tamil',
};

export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && (LOCALES as readonly string[]).includes(v);
}

/** The pseudo-locale formats like English; real locales format in their own language. */
export function baseLocale(locale: MessageLocale): Locale {
  return locale === PSEUDO_LOCALE ? 'en' : locale;
}

/**
 * BCP 47 tag handed to Intl for every number and date: India region (lakh grouping) and `nu-latn`,
 * so digits are Western in every language (₹289, 0.6 km, 4827).
 */
export function intlTag(locale: MessageLocale): string {
  return `${baseLocale(locale)}-IN-u-nu-latn`;
}

/** Value for <html lang>. English keeps the en-IN region tag; the others use the bare language. */
export function htmlLang(locale: MessageLocale): string {
  if (locale === PSEUDO_LOCALE) return PSEUDO_LOCALE;
  return locale === 'en' ? 'en-IN' : locale;
}

/** Locale used for number, date and currency formatting when no language is known yet. */
export const FORMAT_LOCALE = 'en-IN';
