import type { Banner } from '@todayz/api-client/gen/model/banner';
import type { Offer } from '@todayz/api-client/gen/model/offer';
import { formatMoney } from '@todayz/i18n';
import type { PillTone } from '@todayz/ui';
import type { IntlShape } from 'react-intl';

const money = (paise: number) => formatMoney(paise, 'product');

/** "20% off", "₹5 off each", "Buy 2 get 1", "₹50 off bills over ₹500". */
export function offerHeadline(intl: IntlShape, o: Pick<Offer, 'kind' | 'value' | 'buyQty' | 'getQty' | 'minBillPaise'>): string {
  return intl.formatMessage(
    { id: 'offers.kind.summary' },
    { kind: o.kind, pct: o.value, amount: money(o.value), buy: o.buyQty ?? 0, get: o.getQty ?? 0, min: money(o.minBillPaise ?? 0) },
  );
}

/** "Whole shop", "3 items" or "A category". */
export function offerScopeText(intl: IntlShape, o: Pick<Offer, 'scope' | 'storeItemIds'>): string {
  return intl.formatMessage({ id: 'offers.scope.summary' }, { scope: o.scope, count: o.storeItemIds.length });
}

export const offerTone = (status: Offer['status']): PillTone =>
  status === 'live' ? 'accepted' : status === 'scheduled' ? 'pending' : status === 'paused' ? 'preparing' : 'neutral';

export const bannerTone = (status: Banner['status']): PillTone =>
  status === 'live' ? 'accepted' : status === 'scheduled' || status === 'pending_review' ? 'pending' : status === 'paused' ? 'preparing' : status === 'removed' ? 'rejected' : 'neutral';
