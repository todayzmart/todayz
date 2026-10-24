// C-09 / C-11 credits (01 §7): 1 credit = ₹1. The person turns credits on and picks how many.
// The wallet balance and the platform limits are not read here: the server's quote answers with
// `creditsAllowed` and `creditsLimit`, so a "probe" quote that asks for more than anyone can use
// shows the most this cart can take and why it is not more.
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import { useSyncExternalStore } from 'react';

/** The schema's top for `creditsToUse`: far above any limit, so the answer is the real limit. */
export const CREDITS_PROBE = 100000;

interface Choice {
  storeId: string;
  on: boolean;
  amount: number;
}

let choice: Choice | null = null;
const listeners = new Set<() => void>();

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

function set(next: Choice | null): void {
  choice = next;
  listeners.forEach((l) => l());
}

/** The credits asked for this shop's cart: 0 unless credits are on. */
export function useCreditsChoice(storeId: string | undefined): { on: boolean; amount: number } {
  const c = useSyncExternalStore(subscribe, () => choice, () => choice);
  return c && storeId !== undefined && c.storeId === storeId ? { on: c.on, amount: c.amount } : { on: false, amount: 0 };
}

export function setCreditsChoice(storeId: string, on: boolean, amount: number): void {
  set({ storeId, on, amount: Math.max(0, Math.floor(amount)) });
}

export function clearCreditsChoice(): void {
  set(null);
}

/** Tests only. */
export function resetCreditsForTests(): void {
  set(null);
}

/** What the credits control should say, from the probe quote. */
export type CreditsState =
  | { kind: 'hidden' }
  | { kind: 'blocked'; limit: 'credit_monthly_cap' | 'store_credit_cap' | 'below_credit_minimum' }
  | { kind: 'available'; max: number };

export function creditsState(probe: CheckoutQuote | undefined): CreditsState {
  if (!probe) return { kind: 'hidden' };
  if (probe.creditsAllowed > 0) return { kind: 'available', max: probe.creditsAllowed };
  switch (probe.creditsLimit) {
    case 'credit_monthly_cap':
    case 'store_credit_cap':
    case 'below_credit_minimum':
      return { kind: 'blocked', limit: probe.creditsLimit };
    default:
      // `not_available`, `no_balance` or nothing said: nothing to offer.
      return { kind: 'hidden' };
  }
}
