// C-09 cart: one shop at a time (01 §6 "one order = one store"). The lead set this interface so
// W2-08 (Add to cart on C-05, C-07, C-08) and W2-09 (cart, checkout) build on the same module.
// W2-09 owns this folder: it keeps the exported names and types and replaces the in-memory
// state with IndexedDB so the cart survives reloads and sign-in (01 §2).
import { useSyncExternalStore } from 'react';

export interface CartStore {
  id: string;
  slug: string;
  name: string;
}

export interface CartLine {
  storeItemId: string;
  productName: string;
  packLabel?: string;
  pricePaise: number;
  mrpPaise?: number;
  imageUrl?: string;
  qty: number;
}

export interface CartState {
  store: CartStore | null;
  lines: readonly CartLine[];
}

/** `other_store`: the cart holds another shop's items; the caller asks to replace the cart (C-09). */
export type AddResult = { ok: true } | { ok: false; reason: 'other_store'; current: CartStore };

const EMPTY: CartState = { store: null, lines: [] };
const listeners = new Set<() => void>();
let state: CartState = EMPTY;

function set(next: CartState): void {
  state = next.lines.length === 0 ? EMPTY : next;
  listeners.forEach((l) => l());
}

export function getCart(): CartState {
  return state;
}

export function addToCart(store: CartStore, line: Omit<CartLine, 'qty'>, qty = 1): AddResult {
  if (state.store && state.store.id !== store.id) return { ok: false, reason: 'other_store', current: state.store };
  const existing = state.lines.find((l) => l.storeItemId === line.storeItemId);
  const lines = existing
    ? state.lines.map((l) => (l === existing ? { ...l, ...line, qty: l.qty + qty } : l))
    : [...state.lines, { ...line, qty }];
  set({ store, lines });
  return { ok: true };
}

/** Empties the cart and starts it again with this shop's line (the "replace cart" answer). */
export function replaceCart(store: CartStore, line: Omit<CartLine, 'qty'>, qty = 1): void {
  set({ store, lines: [{ ...line, qty }] });
}

/** A quantity of 0 or less removes the line. */
export function setQty(storeItemId: string, qty: number): void {
  const lines = qty > 0
    ? state.lines.map((l) => (l.storeItemId === storeItemId ? { ...l, qty } : l))
    : state.lines.filter((l) => l.storeItemId !== storeItemId);
  set({ store: state.store, lines });
}

export function clearCart(): void {
  set(EMPTY);
}

export function cartCount(c: CartState): number {
  return c.lines.reduce((n, l) => n + l.qty, 0);
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useCart(): CartState {
  return useSyncExternalStore(subscribe, getCart, getCart);
}
