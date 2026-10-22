import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages, pseudoAllowed, resolveLocale, type MessageLocale, type Messages } from '@todayz/i18n';

/** English ships in the entry chunk; the pseudo-locale loads only when asked for. */
export async function loadMessages(locale: MessageLocale): Promise<Messages> {
  if (locale === 'en-XA') {
    const mod = await import('@todayz/i18n/compiled/business/en-XA.json');
    return asMessages(mod.default);
  }
  return asMessages(en);
}

/** `?locale=pseudo` works in every build mode except production (the e2e build uses mode `e2e`). */
export function initialLocale(search = window.location.search, mode: string = import.meta.env.MODE): MessageLocale {
  return resolveLocale(search, { allowPseudo: pseudoAllowed(mode) });
}
