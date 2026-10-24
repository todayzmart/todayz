/**
 * Insight deep links for B-07 (D-99, 06-screens.md "Insight deep links"). An insight action opens
 * Stock with query parameters; they prefill a form or a mode and apply nothing. An id that is not in
 * this store, or a parameter the person's role may not use, is ignored by the screen, and unknown
 * parameters are ignored here.
 */
import { MAX_QUANTITY } from './format';

export interface StockSearch {
  /** `restock=<itemId>&qty=<n>`: the restock form for that item. */
  restock?: string;
  qty?: number;
  /** `count=<id>,<id>,…`: count mode with these items (at most 100). */
  count?: string[];
  /** `expire=<itemId>&batches=<id>,<id>,…`: "Mark expired" with those batches ticked. */
  expire?: string;
  batches?: string[];
  /** `price=<itemId>&ref=<paise>`: the price editor with a reference price. */
  price?: string;
  ref?: number;
  /** `batch=<batchId>&item=<itemId>`: the edit form of one batch ("Check cost"). */
  batch?: string;
  item?: string;
  /** `add=<masterProductId>` or `add_q=<text>`: add item prefilled. */
  add?: string;
  add_q?: string;
}

export const MAX_COUNT_IDS = 100;
const ID = /^[A-Za-z0-9_-]{1,40}$/;

const text = (v: unknown): string | undefined => (typeof v === 'string' || typeof v === 'number' ? String(v) : undefined);

function id(v: unknown): string | undefined {
  const s = text(v);
  return s !== undefined && ID.test(s) ? s : undefined;
}

function ids(v: unknown, max = MAX_COUNT_IDS): string[] | undefined {
  const s = text(v);
  if (s === undefined) return undefined;
  const out: string[] = [];
  for (const part of s.split(',')) {
    const one = part.trim();
    if (ID.test(one) && !out.includes(one)) out.push(one);
    if (out.length === max) break;
  }
  return out.length > 0 ? out : undefined;
}

function whole(v: unknown, min: number, max: number): number | undefined {
  const s = text(v);
  if (s === undefined || !/^\d{1,9}$/.test(s)) return undefined;
  const n = Number(s);
  return n >= min && n <= max ? n : undefined;
}

/** TanStack Router `validateSearch` for `/stock`: keeps only well-formed known parameters. */
export function validateStockSearch(raw: Record<string, unknown>): StockSearch {
  const out: StockSearch = {};
  const restock = id(raw.restock);
  if (restock) {
    out.restock = restock;
    const qty = whole(raw.qty, 1, MAX_QUANTITY);
    if (qty !== undefined) out.qty = qty;
  }
  const count = ids(raw.count);
  if (count) out.count = count;
  const expire = id(raw.expire);
  if (expire) {
    out.expire = expire;
    const batches = ids(raw.batches);
    if (batches) out.batches = batches;
  }
  const price = id(raw.price);
  if (price) {
    out.price = price;
    const ref = whole(raw.ref, 1, 1_000_000_000);
    if (ref !== undefined) out.ref = ref;
  }
  const batch = id(raw.batch);
  const item = id(raw.item);
  if (batch && item) {
    out.batch = batch;
    out.item = item;
  }
  const add = id(raw.add);
  if (add) out.add = add;
  const addQ = text(raw.add_q)?.trim().slice(0, 80);
  if (!add && addQ) out.add_q = addQ;
  return out;
}

/**
 * "Mark expired" with batches ticked, earliest expiry first, each at min(batch remaining, available
 * left), so the total never exceeds `available` (D-99). Batches not in `batches` are skipped.
 */
export function planExpiry(
  batches: readonly { id: string; quantity: number; expiresOn: string | null }[],
  wanted: readonly string[],
  available: number,
): { id: string; take: number }[] {
  const byId = new Map(batches.map((b) => [b.id, b]));
  const chosen = wanted
    .map((w) => byId.get(w))
    .filter((b): b is (typeof batches)[number] => b !== undefined && b.quantity > 0)
    .sort((a, b) => (a.expiresOn ?? '9999').localeCompare(b.expiresOn ?? '9999'));
  let left = Math.max(0, available);
  const plan: { id: string; take: number }[] = [];
  for (const b of chosen) {
    const take = Math.min(b.quantity, left);
    if (take <= 0) continue;
    plan.push({ id: b.id, take });
    left -= take;
  }
  return plan;
}
