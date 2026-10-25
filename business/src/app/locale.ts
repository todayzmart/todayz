// The interface language: change it and remember it on this device (B-14 Settings).
import { applyLocale, browserStore, saveLocale, type Locale, type MessageLocale } from '@todayz/i18n';
import { createContext, useContext } from 'react';

export interface LocaleApi {
  locale: MessageLocale;
  /** The person chose a language. Resolves when its catalogue is on screen. */
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
