// Turns the cart into the sale that goes in the outbox.
import { amountDue, creditsAllowed, creditsToPaise, toSaleLine } from './money';
import { cartTotals, type CartState } from './cart';
import type { PosSettings, Sale } from './types';

/** Credits the bill QR claim may take off: only on bills that reach the minimum (D-30). */
export function creditsFor(cart: CartState, settings: Pick<PosSettings, 'creditMinOrderValuePaise'>): number {
  if (cart.link?.method !== 'bill_qr') return 0;
  const t = cartTotals(cart);
  if (!creditsAllowed(t.netPaise, settings.creditMinOrderValuePaise)) return 0;
  // The bill stays at ₹1 or more after credits (01 §7).
  const room = Math.max(0, Math.floor((t.totalPaise - 100) / 100));
  return Math.min(cart.link.creditsUsed, room);
}

export function buildSale(
  cart: CartState,
  settings: Pick<PosSettings, 'creditMinOrderValuePaise'>,
  clientId: string,
  soldAt: string,
): Omit<Sale, 'deviceSeq' | 'deviceId'> {
  const t = cartTotals(cart);
  const credits = creditsFor(cart, settings);
  const link = cart.link?.method === 'bill_qr' ? { ...cart.link, creditsUsed: credits } : cart.link;
  return {
    clientId,
    soldAt,
    lines: cart.lines.map((l) => toSaleLine(l.id, l)),
    subtotalPaise: t.subtotalPaise,
    billDiscountPaise: t.billDiscountPaise,
    roundOffPaise: t.roundOffPaise,
    creditsUsed: credits,
    totalPaise: amountDue(t.totalPaise, creditsToPaise(credits)),
    paymentMethod: cart.paymentMethod,
    link,
  };
}
