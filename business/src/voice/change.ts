import { formatMoney } from '@todayz/i18n';
import type { IntlShape } from 'react-intl';
import type { Effective } from './lines';

/** "Sold 2", "Restock +10", "Out of stock", "Price ₹58 (now ₹60)". */
export function changeText(intl: IntlShape, e: Effective, currentPricePaise: number | null): string {
  switch (e.kind) {
    case 'sale':
      return intl.formatMessage({ id: 'voice.change.sale' }, { n: e.quantity ?? 0 });
    case 'restock':
      return intl.formatMessage({ id: 'voice.change.restock' }, { n: e.quantity ?? 0 });
    case 'count':
      return e.quantity === 0
        ? intl.formatMessage({ id: 'voice.change.out' })
        : intl.formatMessage({ id: 'voice.change.count' }, { n: e.quantity ?? 0 });
    case 'set_price':
      if (e.pricePaise === null) return intl.formatMessage({ id: 'voice.change.priceMissing' });
      return currentPricePaise !== null
        ? intl.formatMessage({ id: 'voice.change.priceFrom' }, { price: formatMoney(e.pricePaise), now: formatMoney(currentPricePaise) })
        : intl.formatMessage({ id: 'voice.change.price' }, { price: formatMoney(e.pricePaise) });
  }
}
