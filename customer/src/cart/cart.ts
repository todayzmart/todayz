// C-09 cart: one shop at a time (01 §6 "one order = one store"). The lead set this interface so
// W2-08 (Add to cart on C-05, C-07, C-08) and W2-09 (cart, checkout) build on the same module.
// W2-09 owns this folder: it keeps the exported names and types. The state is read synchronously
// from memory and written through to IndexedDB (storage.ts), so the cart survives reloads and
// sign-in (01 §2). Nothing about the person is kept here: no phone, no token, no pickup code.
import { useSyncExternalStore } from 'react';
import { indexedDbStorage, type CartStorage } from './storage';

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
  /** Booked from a shop's Yes on an ask (C-26, W3-16): quote and order price this line at the reply. */
  askReplyId?: string;
  /** The asked quantity: the reply books at most this many. Above it the line drops the reply and is a plain line. */
  askQty?: number;
  /** The reply has a price: checkout drops the reply once the quote stops pricing the line at it. */
  askPriced?: true;
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
let storage: CartStorage | null = null;
let touched = false;
let ready: Promise<void> = Promise.resolve();
let writes: Promise<void> = Promise.resolve();

function emit(): void {
  listeners.forEach((l) => l());
}

function plainLine(l: CartLine): CartLine {
  const { askReplyId: _reply, askQty: _qty, askPriced: _priced, ...plain } = l;
  return plain;
}

/**
 * A line past its ask's quantity no longer books from the reply, so checkout never sends a quantity the
 * reply refuses. A reply without a valid asked quantity (an older or damaged record) is dropped too.
 */
function fitAsk(l: CartLine): CartLine {
  if (l.askReplyId === undefined && l.askQty === undefined && l.askPriced === undefined) return l;
  const fits =
    typeof l.askReplyId === 'string' && Number.isInteger(l.askQty) && (l.askQty ?? 0) > 0 && l.qty <= (l.askQty ?? 0);
  return fits ? l : plainLine(l);
}

function set(next: CartState): void {
  state = next.lines.length === 0 ? EMPTY : { store: next.store, lines: next.lines.map(fitAsk) };
  touched = true;
  emit();
  const s = storage;
  if (s) {
    const snapshot = state;
    // Writes go one after the other so an older snapshot never lands last.
    writes = writes.then(() => s.save(snapshot)).catch(() => undefined);
  }
}

/** Valid lines only: a record from an older build or a damaged one never breaks the page. */
function clean(v: CartState): CartState {
  const lines = v.lines.filter(
    (l) =>
      typeof l.storeItemId === 'string' &&
      typeof l.productName === 'string' &&
      Number.isInteger(l.pricePaise) &&
      Number.isInteger(l.qty) &&
      l.qty > 0,
  ).map(fitAsk);
  return v.store && lines.length > 0 ? { store: v.store, lines } : EMPTY;
}

/**
 * Points the cart at a storage and loads what it holds. A change made before the load finishes
 * wins: the saved lines are merged in only when they are the same shop's.
 */
export function attachCartStorage(next: CartStorage | null): Promise<void> {
  storage = next;
  touched = false;
  if (!next) {
    ready = Promise.resolve();
    return ready;
  }
  ready = next
    .load()
    .then((loaded) => {
      const saved = loaded ? clean(loaded) : EMPTY;
      if (saved === EMPTY) return;
      if (!touched || state === EMPTY) {
        state = saved;
      } else if (state.store && saved.store && state.store.id === saved.store.id) {
        const have = new Set(state.lines.map((l) => l.storeItemId));
        state = { store: state.store, lines: [...state.lines, ...saved.lines.filter((l) => !have.has(l.storeItemId))] };
      } else {
        return;
      }
      emit();
    })
    .catch(() => undefined);
  return ready;
}

/** Resolves when the saved cart has been read and the last write has landed. */
export function cartSettled(): Promise<void> {
  return ready.then(() => writes);
}

/** Tests only: forget everything in memory. */
export function resetCartForTests(): void {
  state = EMPTY;
  storage = null;
  touched = false;
  ready = Promise.resolve();
  writes = Promise.resolve();
  emit();
}

// Start-up: the browser's IndexedDB, except under test where each test attaches its own storage.
if (import.meta.env.MODE !== 'test') void attachCartStorage(indexedDbStorage());

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

/**
 * The reply can no longer price its line (checkout's 409 `ask_price_expired` or `ask_reply_used`, or a quote
 * at another price): the line stays, at the current price.
 */
export function dropAskReply(): void {
  if (askReplyOf(state) === undefined) return;
  set({ store: state.store, lines: state.lines.map(plainLine) });
}

/** The ask reply the cart books from, if a line came from one (one per order, 01 §11.2). */
export function askReplyOf(c: CartState): string | undefined {
  return c.lines.find((l) => typeof l.askReplyId === 'string')?.askReplyId;
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
