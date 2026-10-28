// The bill being rung up, kept for the tab (D-279): leaving Sell for another page, or a reload,
// brings the same lines and the same bill id back. sessionStorage, so it goes with the tab; sign-out
// clears it (useSignOut). No customer phone is kept, only what is on the bill.
import type { CartState } from './cart';

const PREFIX = 'todayz.biz.pos.openBill.';

export interface OpenBill {
  saleId: string;
  cart: CartState;
}

export function loadOpenBill(storeId: string): OpenBill | null {
  try {
    const raw = sessionStorage.getItem(PREFIX + storeId);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<OpenBill>;
    if (typeof v.saleId !== 'string' || !v.cart || !Array.isArray(v.cart.lines)) return null;
    return { saleId: v.saleId, cart: v.cart };
  } catch {
    return null;
  }
}

/** Keeps a bill with lines; an empty bill removes the entry. */
export function saveOpenBill(storeId: string, bill: OpenBill) {
  try {
    if (bill.cart.lines.length === 0) sessionStorage.removeItem(PREFIX + storeId);
    else sessionStorage.setItem(PREFIX + storeId, JSON.stringify(bill));
  } catch {
    // Private mode or full storage: the bill still lives while Sell is open.
  }
}

export function clearOpenBills() {
  try {
    const keys: string[] = [];
    for (let i = 0; i < sessionStorage.length; i++) keys.push(sessionStorage.key(i) ?? '');
    for (const k of keys) if (k.startsWith(PREFIX)) sessionStorage.removeItem(k);
  } catch {
    // Nothing to clear.
  }
}
