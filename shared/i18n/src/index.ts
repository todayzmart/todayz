// @todayz/i18n bootstrap (W0-05). Locale choice and message loading for the apps.
// W1-03 replaces this with the full setup: five languages, detection, persistence, formatters.
import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';

/** Message locales available before W1-03: English and the en-XA pseudo-locale. */
export type MessageLocale = 'en' | 'en-XA';

/** Compiled messages: key -> ICU AST, as produced by scripts/gen.mjs. */
export type Messages = Record<string, MessageFormatElement[]>;

/** Locale used for number, date and currency formatting (₹, Indian grouping, IST). */
export const FORMAT_LOCALE = 'en-IN';

export const PSEUDO_LOCALE = 'en-XA' satisfies MessageLocale;

/**
 * `?locale=pseudo` or `?locale=en-XA` selects the pseudo-locale; anything else is English. Off
 * unless `allowPseudo` is set: production builds leave it off, so the switch exists only in dev,
 * unit tests and the e2e build (see pseudoAllowed).
 */
export function resolveLocale(search: string, { allowPseudo = false }: { allowPseudo?: boolean } = {}): MessageLocale {
  if (!allowPseudo) return 'en';
  const v = new URLSearchParams(search).get('locale')?.toLowerCase();
  return v === 'pseudo' || v === 'en-xa' ? 'en-XA' : 'en';
}

/** The pseudo-locale is available in every Vite mode except `production`. */
export function pseudoAllowed(mode: string): boolean {
  return mode !== 'production';
}

/** Value for <html lang>. The pseudo-locale is still English underneath. */
export function htmlLang(locale: MessageLocale): string {
  return locale === 'en-XA' ? 'en-XA' : 'en-IN';
}

/** Casts a compiled JSON import to Messages (JSON imports widen the AST enums to number). */
export function asMessages(json: unknown): Messages {
  return json as Messages;
}
