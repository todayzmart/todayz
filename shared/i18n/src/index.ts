// @todayz/i18n public API (W1-03). Documented in README.md.
import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';
import { localeFromSearch } from './detect';
import { loadFonts } from './fonts';
import { formatAbsolute, formatDistance, formatMoney, formatRelative, type DateInput, type MoneyStyle } from './format';
import { PSEUDO_LOCALE, htmlLang, type MessageLocale } from './locales';

export * from './locales';
export * from './detect';
export * from './fonts';
export * from './format';

/** Compiled messages: key -> ICU AST, as produced by scripts/gen.mjs. */
export type Messages = Record<string, MessageFormatElement[]>;

/** Casts a compiled JSON import to Messages (JSON imports widen the AST enums to number). */
export function asMessages(json: unknown): Messages {
  return json as Messages;
}

/**
 * The pseudo-locale switch only: `?locale=pseudo` gives en-XA when `allowPseudo` is set, anything
 * else is English. A real language in the URL is ignored here (the apps' existing contract); use
 * resolveLanguage or detectLocale for that.
 */
export function resolveLocale(search: string, { allowPseudo = false }: { allowPseudo?: boolean } = {}): MessageLocale {
  const found = localeFromSearch(search, { allowPseudo });
  return found === PSEUDO_LOCALE ? found : 'en';
}

/**
 * Language from `?locale=` alone (deep links, tests): a language code, or the pseudo-locale when
 * `allowPseudo` is set; anything else is English. Apps normally call detectLocale, which adds the
 * stored choice and the browser's languages.
 */
export function resolveLanguage(search: string, { allowPseudo = false }: { allowPseudo?: boolean } = {}): MessageLocale {
  return localeFromSearch(search, { allowPseudo }) ?? 'en';
}

/** The pseudo-locale is available in every Vite mode except `production`. */
export function pseudoAllowed(mode: string): boolean {
  return mode !== 'production';
}

/**
 * Puts a language on the page: `<html lang>` (the CSS raises line height for hi, kn, te and ta
 * from it) and starts loading that script's fonts. Safe to call outside a browser.
 */
export async function applyLocale(locale: MessageLocale, doc: Pick<Document, 'documentElement'> | undefined = globalThis.document): Promise<void> {
  if (doc) doc.documentElement.lang = htmlLang(locale);
  await loadFonts(locale);
}

/** Formatters bound to one language, so components do not pass it around. `now` is injectable for tests. */
export function createFormatters(locale: MessageLocale, now: () => number = Date.now) {
  return {
    money: (paise: number, style?: MoneyStyle) => formatMoney(paise, style),
    distance: (metres: number) => formatDistance(metres),
    relative: (then: DateInput) => formatRelative(then, now(), locale),
    absolute: (then: DateInput) => formatAbsolute(then, now(), locale),
  };
}
