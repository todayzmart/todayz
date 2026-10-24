/**
 * Insight deep links for B-08 (D-99, 06-screens.md "Insight deep links"). An insight action opens
 * Offers with query parameters; they prefill the offer form and create nothing. An id that is not in
 * this store is ignored by the screen, and unknown parameters are ignored here.
 *
 * - `offer=item&item=<storeItemId>&pct=<whole number>`
 * - `offer=flash&batches=<batchId>,<batchId>,…` (at most 100)
 * - `offer=pair&items=<storeItemId>,<storeItemId>`
 */
import { percentInLimits } from './limits';

export interface OffersSearch {
  offer?: 'item' | 'flash' | 'pair';
  item?: string;
  /** Only when it is a whole number inside the safe limits. */
  pct?: number;
  batches?: string[];
  items?: string[];
}

export const MAX_FLASH_BATCHES = 100;
const ID = /^[A-Za-z0-9_-]{1,40}$/;

const text = (v: unknown): string | undefined => (typeof v === 'string' || typeof v === 'number' ? String(v) : undefined);

function id(v: unknown): string | undefined {
  const s = text(v);
  return s !== undefined && ID.test(s) ? s : undefined;
}

function ids(v: unknown, max: number): string[] {
  const s = text(v);
  if (s === undefined) return [];
  const out: string[] = [];
  for (const part of s.split(',')) {
    const one = part.trim();
    if (ID.test(one) && !out.includes(one)) out.push(one);
    if (out.length === max) break;
  }
  return out;
}

/** TanStack Router `validateSearch` for `/offers`: keeps only a well-formed, known link. */
export function validateOffersSearch(raw: Record<string, unknown>): OffersSearch {
  const kind = text(raw.offer);
  if (kind === 'item') {
    const item = id(raw.item);
    if (!item) return {};
    const pctText = text(raw.pct);
    const pct = pctText !== undefined && /^\d{1,3}$/.test(pctText) ? Number(pctText) : undefined;
    return { offer: 'item', item, ...(pct !== undefined && percentInLimits(pct) ? { pct } : {}) };
  }
  if (kind === 'flash') {
    const batches = ids(raw.batches, MAX_FLASH_BATCHES);
    return batches.length > 0 ? { offer: 'flash', batches } : {};
  }
  if (kind === 'pair') {
    const items = ids(raw.items, 2);
    return items.length === 2 ? { offer: 'pair', items } : {};
  }
  return {};
}
