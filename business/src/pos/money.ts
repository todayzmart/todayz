// Bill arithmetic for the counter. All amounts are integer paise.
// Round-off is automatic, to the nearest ₹1, and half rounds up (D-94, 04 §POS): ₹100.49 -> ₹100
// (-49), ₹100.50 -> ₹101 (+50), so `roundOffPaise` is always within -49…+50. Cashiers get it too (D-65).
// A bill never goes below ₹1 through discounts (01 §13 pricing rule, POS-16).
import type { SaleLine } from './types';

const RUPEE = 100;

export interface CartLineInput {
  unitPricePaise: number;
  quantity: number;
  /** Manual discount on this line, in paise. */
  discountPaise: number;
}

export interface BillTotals {
  /** Σ line totals after line discounts. */
  subtotalPaise: number;
  /** The bill discount actually applied (capped so the bill stays at ₹1 or more). */
  billDiscountPaise: number;
  /** subtotal − discount, before round-off. */
  netPaise: number;
  roundOffPaise: number;
  /** net + round-off: the amount before credits. */
  totalPaise: number;
  itemCount: number;
}

/** One line's total: price × quantity less its discount, never below 0. */
export function lineTotal(l: CartLineInput): number {
  const gross = Math.round(l.unitPricePaise * l.quantity);
  return Math.max(0, gross - Math.max(0, Math.min(l.discountPaise, gross)));
}

/** Round to the nearest ₹1, half rounding up. Returns the signed adjustment in paise. */
export function roundOff(netPaise: number): number {
  if (netPaise <= 0) return 0;
  const r = netPaise % RUPEE;
  const adj = r >= 50 ? RUPEE - r : 0 - r;
  // A bill under 50 paise would round to ₹0; leave it as it is.
  return netPaise + adj < 1 ? 0 : adj;
}

export function billTotals(lines: readonly CartLineInput[], billDiscountPaise: number): BillTotals {
  const subtotalPaise = lines.reduce((s, l) => s + lineTotal(l), 0);
  // The bill never goes below ₹1: the discount can take at most subtotal − ₹1 (nothing under ₹1).
  const maxDiscount = Math.max(0, subtotalPaise - RUPEE);
  const applied = Math.max(0, Math.min(Math.round(billDiscountPaise), maxDiscount));
  const netPaise = subtotalPaise - applied;
  const adj = roundOff(netPaise);
  return {
    subtotalPaise,
    billDiscountPaise: applied,
    netPaise,
    roundOffPaise: adj,
    totalPaise: netPaise + adj,
    itemCount: lines.reduce((n, l) => n + l.quantity, 0),
  };
}

/** The credit minimum (D-30) is checked on the bill after discounts, before round-off and credits. */
export function creditsAllowed(netPaise: number, creditMinOrderValuePaise: number): boolean {
  return netPaise >= creditMinOrderValuePaise;
}

/** What the customer pays once a bill QR claim has taken credits off. */
export function amountDue(totalPaise: number, creditsUsed: number): number {
  return Math.max(0, totalPaise - Math.max(0, creditsUsed));
}

/** Rupees typed in a discount box ("12.50") to paise; null when it is not an amount. */
export function parseRupees(text: string): number | null {
  const t = text.trim().replace(/^₹/, '').replace(/,/g, '');
  if (!/^\d+(\.\d{1,2})?$/.test(t)) return null;
  const [whole = '0', frac = ''] = t.split('.');
  return Number(whole) * RUPEE + Number(frac.padEnd(2, '0'));
}

export function toSaleLine(
  id: string,
  l: { storeItemId: string | null; name: string; unitPricePaise: number; quantity: number; discountPaise: number },
): SaleLine {
  return {
    clientId: id,
    storeItemId: l.storeItemId,
    name: l.name,
    unitPricePaise: l.unitPricePaise,
    quantity: l.quantity,
    discountPaise: l.discountPaise,
    lineTotalPaise: lineTotal(l),
  };
}
