import type { OfferCreate } from '@todayz/api-client/gen/model/offerCreate';
import type { SampleLine } from '@todayz/api-client/gen/model/sampleLine';
import type { PickedItem } from './form';

/** The preview prices a small cart, not the shop's whole range. */
export const MAX_SAMPLE_LINES = 10;
const MAX_QTY = 99;
const POOL_LINES = 3;

/**
 * A sample cart for the live preview: the items the offer names (qty 1), else the first items of the
 * category or shop. Buy X get Y gets enough units to trigger, and a minimum-bill offer gets enough
 * of the first line to reach the minimum, so the preview shows the offer working.
 */
export function sampleLines(body: OfferCreate, picked: readonly PickedItem[], pool: readonly PickedItem[]): SampleLine[] {
  const known = new Map<string, PickedItem>([...pool, ...picked].map((i) => [i.id, i]));
  const ids = body.storeItemIds && body.storeItemIds.length > 0 ? body.storeItemIds : pool.slice(0, POOL_LINES).map((i) => i.id);
  const lines: SampleLine[] = ids.slice(0, MAX_SAMPLE_LINES).map((storeItemId) => ({ storeItemId, quantity: 1 }));
  const first = lines[0];
  if (!first) return [];
  if (body.kind === 'bxgy' && body.buyQty && body.getQty) first.quantity = Math.min(MAX_QTY, body.buyQty + body.getQty);
  if (body.minBillPaise) {
    const price = known.get(first.storeItemId)?.pricePaise ?? 0;
    const others = lines.slice(1).reduce((sum, l) => sum + (known.get(l.storeItemId)?.pricePaise ?? 0) * l.quantity, 0);
    if (price > 0 && price + others < body.minBillPaise) first.quantity = Math.min(MAX_QTY, Math.ceil((body.minBillPaise - others) / price));
  }
  return lines;
}
