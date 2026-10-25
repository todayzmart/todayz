// The C-26 progress line, shared by the ask page and the list.
import type { Ask } from '@todayz/api-client/gen/model/ask';
import { formatMoney } from '@todayz/i18n';
import type { IntlShape } from 'react-intl';

/** "Asked {n} shops · {r} replied · best ₹{p}"; the best part only once a Yes has a price. */
export function progressLine(intl: IntlShape, ask: Ask): string {
  const { asked, replied, bestPricePaise } = ask.progress;
  return bestPricePaise !== null
    ? intl.formatMessage({ id: 'asks.progress.best' }, { n: asked, r: replied, price: formatMoney(bestPricePaise) })
    : intl.formatMessage({ id: 'asks.progress' }, { n: asked, r: replied });
}
