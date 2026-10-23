import type { ApiError } from '@todayz/api-client';
import { quoteCheckout } from '@todayz/api-client/gen/orders/orders';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import { useQuery } from '@tanstack/react-query';
import type { CartState } from './cart';

/** Pickup only this weekend. TODO(W2-03): delivery and the address picker. */
export const FULFILMENT = 'pickup' as const;

export function quoteKey(cart: CartState): readonly unknown[] {
  return ['quote', cart.store?.id ?? '', cart.lines.map((l) => `${l.storeItemId}:${l.qty}`).join(',')];
}

/**
 * The priced cart (D-02): no side effects, so it is asked again whenever the cart changes. Signed
 * out there is nothing to ask (the route is a /me route).
 */
export function useCartQuote(cart: CartState, enabled: boolean) {
  return useQuery<CheckoutQuote, ApiError>({
    queryKey: quoteKey(cart),
    enabled: enabled && cart.store !== null && cart.lines.length > 0,
    retry: false,
    staleTime: 0,
    queryFn: () =>
      quoteCheckout({
        storeId: cart.store?.id ?? '',
        fulfilment: FULFILMENT,
        items: cart.lines.map((l) => ({ storeItemId: l.storeItemId, quantity: l.qty })),
        creditsToUse: 0, // TODO(W2-01): credits are not built; the toggle is hidden.
      }),
  });
}

const DAY_KEYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const;

/** Today's opening ranges ("09:00–21:00") from the shop page's weekly hours, in IST at `serverNow`. */
export function todaysHours(weekly: Record<string, { open: string; close: string }[]>, serverNow: string): string[] {
  const ist = new Date(Date.parse(serverNow) + 5.5 * 60 * 60 * 1000);
  const key = DAY_KEYS[ist.getUTCDay()] ?? 'mon';
  return (weekly[key] ?? []).map((r) => `${r.open}–${r.close}`);
}
