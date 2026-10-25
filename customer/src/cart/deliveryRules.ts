// The delivery rules the customer screens say in words (01 §5.5, D-94): the quote's refusals and the
// shop's terms. The server decides; this only reads its answer.
import { ApiError } from '@todayz/api-client';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import type { NextDeliveryWindow } from '@todayz/api-client/gen/model/nextDeliveryWindow';
import { formatMoney } from '@todayz/i18n';
import type { IntlShape } from 'react-intl';

export type DeliveryProblem =
  | { kind: 'delivery_closed'; next: NextDeliveryWindow | null }
  | { kind: 'below_minimum'; minimumPaise: number; itemSubtotalPaise: number | null }
  | { kind: 'outside_radius' };

/** The quote's or the order's delivery refusal, or null for any other answer. */
export function deliveryProblemOf(err: unknown): DeliveryProblem | null {
  if (!(err instanceof ApiError)) return null;
  const ext = err.problem.extensions ?? {};
  switch (err.code) {
    case 'delivery_closed': {
      const w = ext['nextDeliveryWindow'] as Partial<NextDeliveryWindow> | null | undefined;
      return {
        kind: 'delivery_closed',
        next: w && typeof w.opensAt === 'string' && typeof w.closesAt === 'string' ? { opensAt: w.opensAt, closesAt: w.closesAt } : null,
      };
    }
    case 'below_minimum':
      return {
        kind: 'below_minimum',
        minimumPaise: typeof ext['minimumPaise'] === 'number' ? ext['minimumPaise'] : 0,
        itemSubtotalPaise: typeof ext['itemSubtotalPaise'] === 'number' ? ext['itemSubtotalPaise'] : null,
      };
    case 'store_not_bookable':
      return ext['reason'] === 'outside_delivery_radius' ? { kind: 'outside_radius' } : null;
    default:
      return null;
  }
}

/** "₹30 delivery · Minimum ₹150 · Free delivery on ₹500+": the shop's terms, from its page. */
export function deliveryTerms(store: DiscoverStorePage, intl: IntlShape): string {
  const parts = [
    store.deliveryFeePaise > 0
      ? intl.formatMessage({ id: 'delivery.terms.fee' }, { amount: formatMoney(store.deliveryFeePaise) })
      : intl.formatMessage({ id: 'delivery.terms.noFee' }),
  ];
  if (store.deliveryMinOrderPaise > 0) {
    parts.push(intl.formatMessage({ id: 'delivery.terms.minimum' }, { amount: formatMoney(store.deliveryMinOrderPaise) }));
  }
  if (store.deliveryFreeAbovePaise !== null && store.deliveryFeePaise > 0) {
    parts.push(intl.formatMessage({ id: 'delivery.terms.freeAbove' }, { amount: formatMoney(store.deliveryFreeAbovePaise) }));
  }
  return parts.join(' · ');
}

/** What is left before the fee drops to 0, while delivery is quoted with a fee. */
export function freeDeliveryGap(store: DiscoverStorePage | undefined, q: CheckoutQuote | undefined): number | null {
  if (!store || !q || q.fulfilment !== 'delivery' || q.deliveryFeePaise === 0 || store.deliveryFreeAbovePaise === null) return null;
  const gap = store.deliveryFreeAbovePaise - q.itemSubtotalPaise;
  return gap > 0 ? gap : null;
}
