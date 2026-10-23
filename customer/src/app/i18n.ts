import en from '@todayz/i18n/compiled/customer/en.json';
import {
  asMessages,
  browserStore,
  detectLocale,
  pseudoAllowed,
  type MessageLocale,
  type Messages,
} from '@todayz/i18n';

/** English ships in the entry chunk; every other catalogue loads only when it is chosen. */
export async function loadMessages(locale: MessageLocale): Promise<Messages> {
  switch (locale) {
    case 'en-XA':
      return asMessages((await import('@todayz/i18n/compiled/customer/en-XA.json')).default);
    case 'hi':
      return asMessages((await import('@todayz/i18n/compiled/customer/hi.json')).default);
    case 'kn':
      return asMessages((await import('@todayz/i18n/compiled/customer/kn.json')).default);
    case 'te':
      return asMessages((await import('@todayz/i18n/compiled/customer/te.json')).default);
    case 'ta':
      return asMessages((await import('@todayz/i18n/compiled/customer/ta.json')).default);
    default:
      return asMessages(en);
  }
}

/**
 * The language to start in: the stored choice, then `?locale=` (`?locale=pseudo` in every build
 * mode except production), then the browser's languages, then English.
 */
export function initialLocale(search = window.location.search, mode: string = import.meta.env.MODE): MessageLocale {
  return detectLocale({
    store: browserStore(),
    search,
    languages: typeof navigator === 'undefined' ? [] : navigator.languages,
    allowPseudo: pseudoAllowed(mode),
  });
}
