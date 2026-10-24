import type { ApiError } from '@todayz/api-client';
import { quoteCheckout } from '@todayz/api-client/gen/orders/orders';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import type { CartState } from './cart';
import { CREDITS_PROBE, creditsState, useCreditsChoice } from './credits';

/** Pickup only this weekend. TODO(W2-03): delivery and the address picker. */
export const FULFILMENT = 'pickup' as const;

export function quoteKey(cart: CartState, credits = 0): readonly unknown[] {
  return ['quote', cart.store?.id ?? '', cart.lines.map((l) => `${l.storeItemId}:${l.qty}`).join(','), credits];
}

/**
 * The priced cart (D-02): no side effects, so it is asked again whenever the cart changes. Signed
 * out there is nothing to ask (the route is a /me route). `credits` is what the person chose to use.
 */
export function useCartQuote(cart: CartState, enabled: boolean, credits = 0) {
  return useQuery<CheckoutQuote, ApiError>({
    queryKey: quoteKey(cart, credits),
    enabled: enabled && cart.store !== null && cart.lines.length > 0,
    retry: false,
    staleTime: 0,
    // Moving only the credits keeps the last price on screen (marked as a placeholder) instead of a blank.
    placeholderData: (prev, prevQuery) =>
      prevQuery && prevQuery.queryKey.slice(0, 3).join('|') === quoteKey(cart).slice(0, 3).join('|') ? prev : undefined,
    queryFn: () =>
      quoteCheckout({
        storeId: cart.store?.id ?? '',
        fulfilment: FULFILMENT,
        items: cart.lines.map((l) => ({ storeItemId: l.storeItemId, quantity: l.qty })),
        creditsToUse: credits,
      }),
  });
}

/**
 * The same cart asked with more credits than anyone can use: `creditsAllowed` is then the most this
 * cart can take and `creditsLimit` says why it is not more. A failed probe just hides the control.
 */
export function useCreditsProbe(cart: CartState, enabled: boolean) {
  return useCartQuote(cart, enabled, CREDITS_PROBE);
}

/**
 * The cart's price with the credits the person chose (01 §7). The chosen amount is clamped to what the
 * probe says the cart can take and waits a moment while the slider moves, so one drag is one quote.
 */
export function useCartPricing(cart: CartState, enabled: boolean) {
  const choice = useCreditsChoice(cart.store?.id);
  const probe = useCreditsProbe(cart, enabled);
  const state = creditsState(probe.data);
  const wanted = choice.on && state.kind === 'available' ? Math.min(choice.amount, state.max) : 0;
  const [requested, setRequested] = useState(wanted);
  useEffect(() => {
    const t = setTimeout(() => setRequested(wanted), wanted === requested ? 0 : 250);
    return () => clearTimeout(t);
  }, [wanted, requested]);
  const quote = useCartQuote(cart, enabled, requested);
  // The price shown is for `requested`; until it catches up with the slider the page must not place.
  return { quote, probe: probe.data, choice, settling: requested !== wanted || quote.isPlaceholderData };
}

const DAY_KEYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const;

/** Today's opening ranges ("09:00–21:00") from the shop page's weekly hours, in IST at `serverNow`. */
export function todaysHours(weekly: Record<string, { open: string; close: string }[]>, serverNow: string): string[] {
  const ist = new Date(Date.parse(serverNow) + 5.5 * 60 * 60 * 1000);
  const key = DAY_KEYS[ist.getUTCDay()] ?? 'mon';
  return (weekly[key] ?? []).map((r) => `${r.open}–${r.close}`);
}
