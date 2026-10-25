import { formatAbsolute, formatMoney } from '@todayz/i18n';

/** Money in the console's dashboard style (₹1,240). */
export const money = (paise: number) => formatMoney(paise, 'dashboard');

const when = (at: string, now: string) => formatAbsolute(at, now, 'en');

/**
 * A server timestamp against the response's serverNow, never the device clock. The console is
 * English only; the IntlProvider's `en-IN` is a format tag, not a message locale.
 */
export function useWhen() {
  return when;
}
