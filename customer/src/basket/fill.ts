// "Fill cart" (C-22, BSK-10): the covered lines of one shop go into the one-shop cart at their quantities.
// The cart module is W2-09's; this only calls its exported functions. For a split, the second shop's lines
// wait on this device until the first order is placed; C-12 then offers "Next: the rest from {shop}".
import type { BasketCoveredLine } from '@todayz/api-client/gen/model/basketCoveredLine';
import { useSyncExternalStore } from 'react';
import { addToCart, clearCart, getCart, type CartLine, type CartStore } from '../cart/cart';

export type FillLine = Omit<CartLine, 'qty'> & { qty: number };

export function fillLines(covered: readonly BasketCoveredLine[], pack: (l: BasketCoveredLine) => string): FillLine[] {
  return covered.map((c) => {
    const line: FillLine = { storeItemId: c.storeItemId, productName: c.name, packLabel: pack(c), pricePaise: c.pricePaise, qty: c.quantity };
    if (c.mrpPaise !== null && c.mrpPaise > c.pricePaise) line.mrpPaise = c.mrpPaise;
    return line;
  });
}

/** True when filling for `store` would drop another shop's lines: ask first (C-09). */
export function needsReplace(store: CartStore): CartStore | null {
  const cart = getCart();
  return cart.store && cart.store.id !== store.id && cart.lines.length > 0 ? cart.store : null;
}

/** Empties the cart and puts these lines in it for `store`. */
export function fillCart(store: CartStore, lines: readonly FillLine[]): void {
  clearCart();
  for (const { qty, ...line } of lines) addToCart(store, line, qty);
}

// The second half of a split, waiting for the first order.

export interface PendingSplit {
  /** The first shop: C-12 shows the offer on an order from this store. */
  afterStoreId: string;
  store: CartStore;
  lines: FillLine[];
}

const KEY = 'todayz.basket.nextSplit';
const listeners = new Set<() => void>();
let cached: PendingSplit | null | undefined;

function read(): PendingSplit | null {
  if (cached !== undefined) return cached;
  try {
    const raw = localStorage.getItem(KEY);
    const v = raw ? (JSON.parse(raw) as PendingSplit) : null;
    cached = v && typeof v.afterStoreId === 'string' && v.store && Array.isArray(v.lines) && v.lines.length > 0 ? v : null;
  } catch {
    cached = null;
  }
  return cached;
}

export function setPendingSplit(next: PendingSplit | null): void {
  cached = next;
  try {
    if (next) localStorage.setItem(KEY, JSON.stringify(next));
    else localStorage.removeItem(KEY);
  } catch {
    // Storage unavailable: the offer lasts for this visit only.
  }
  listeners.forEach((l) => l());
}

export function resetPendingSplitForTests(): void {
  cached = undefined;
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function usePendingSplit(): PendingSplit | null {
  return useSyncExternalStore(subscribe, read, read);
}
