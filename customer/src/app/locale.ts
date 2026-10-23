// The interface language: change it, remember it on this device, and tell the account when signed in.
import { applyLocale, browserStore, saveLocale, type Locale, type MessageLocale } from '@todayz/i18n';
import { createContext, useContext } from 'react';

export interface LocaleApi {
  locale: MessageLocale;
  /** C-01 / Account: the person chose a language. Resolves when its catalogue is on screen. */
  choose: (locale: Locale) => Promise<void>;
}

export const LocaleContext = createContext<LocaleApi | null>(null);

export function useLocale(): LocaleApi {
  const v = useContext(LocaleContext);
  if (!v) throw new Error('useLocale must be used inside <App>');
  return v;
}

/** The part of a choice that is not React state: store it and put the language on the page. */
export function rememberLocale(locale: Locale): void {
  saveLocale(browserStore(), locale);
  void applyLocale(locale);
}
