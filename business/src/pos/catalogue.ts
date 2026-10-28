// Search and lookup over the offline catalogue snapshot. Everything here is pure and runs on the
// device, so the counter finds items with no network (02 §4.3, B-05 "search/scan bar").
import type { CatalogueItem } from './types';

export const TILE_COUNT = 24;

/** The price the counter charges per unit: the offer price when the server priced one, else the shelf price. */
export function sellPricePaise(item: CatalogueItem): number {
  const offer = item.offerPricePaise;
  return typeof offer === 'number' && offer >= 0 && offer < item.pricePaise ? offer : item.pricePaise;
}

export function hasOffer(item: CatalogueItem): boolean {
  return sellPricePaise(item) < item.pricePaise;
}

function fold(s: string): string {
  return s.toLowerCase().normalize('NFKD').replace(/\p{M}/gu, '').replace(/\s+/g, ' ').trim();
}

export function itemLabel(item: CatalogueItem): string {
  return item.variant ? `${item.name} ${item.variant}` : item.name;
}

/** The item a scanned or typed barcode belongs to, or null. Unlisted items still sell at the counter. */
export function findByBarcode(items: readonly CatalogueItem[], code: string): CatalogueItem | null {
  const c = code.trim();
  if (!c) return null;
  return items.find((i) => i.barcodes.includes(c)) ?? null;
}

/**
 * Ranks items for a typed query: exact barcode first, then names that start with the query, then
 * names that contain every word. Best sellers win ties. At most `limit` results.
 */
export function searchItems(items: readonly CatalogueItem[], query: string, limit = 30): CatalogueItem[] {
  const q = fold(query);
  if (!q) return [];
  const words = q.split(' ');
  const scored: { item: CatalogueItem; score: number }[] = [];
  for (const item of items) {
    if (item.barcodes.some((b) => b === q)) {
      scored.push({ item, score: 0 });
      continue;
    }
    const label = fold(itemLabel(item));
    if (label.startsWith(q)) scored.push({ item, score: 1 });
    else if (words.every((w) => label.includes(w))) scored.push({ item, score: label.split(' ').some((t) => t.startsWith(words[0] ?? '')) ? 2 : 3 });
    else if (q.length >= 3 && item.barcodes.some((b) => b.endsWith(q))) scored.push({ item, score: 4 });
  }
  scored.sort(
    (a, b) =>
      a.score - b.score ||
      (a.item.topRank ?? Number.MAX_SAFE_INTEGER) - (b.item.topRank ?? Number.MAX_SAFE_INTEGER) ||
      itemLabel(a.item).localeCompare(itemLabel(b.item)),
  );
  return scored.slice(0, limit).map((s) => s.item);
}

/**
 * The quick tiles (B-05): the best sellers, best first. The contract has no best-seller feed yet
 * (TODO(lead)), so while no item carries a rank the tiles are the first listed items,
 * alphabetically, so the counter is never an empty grid. An item at 0 keeps its tile with
 * "Out of stock" (D-282), so tiles do not shift as stock runs out.
 */
export function topTiles(items: readonly CatalogueItem[], count = TILE_COUNT): CatalogueItem[] {
  const ranked = items.filter((i) => typeof i.topRank === 'number');
  if (ranked.length > 0) return ranked.sort((a, b) => (a.topRank ?? 0) - (b.topRank ?? 0)).slice(0, count);
  return items
    .filter((i) => i.isListed)
    .sort((a, b) => itemLabel(a).localeCompare(itemLabel(b)))
    .slice(0, count);
}

// Stock at the counter (D-270). The counter never sells past `available`; the server refuses an
// online sale above it, and an offline race that still takes an item below 0 reads 0 with "Count needed".

/** Units set aside for accepted online orders that are not collected or delivered yet. */
export function blockedUnits(item: CatalogueItem): number {
  return Math.max(0, item.reserved ?? 0);
}

/** Units the counter may sell now. */
export function sellableUnits(item: CatalogueItem): number {
  return Math.max(0, item.available);
}

/** Units on the shelf, the blocked ones included. */
export function onHandUnits(item: CatalogueItem): number {
  return Math.max(0, item.onHand ?? sellableUnits(item) + blockedUnits(item));
}

/**
 * The on hand a count sets. A person who counted every unit on the shelf, the ones set aside for
 * online orders too, ticks `includesBlocked`; otherwise the blocked units are added to the count.
 */
export function countedOnHand(counted: number, blocked: number, includesBlocked: boolean): number {
  return includesBlocked ? counted : counted + blocked;
}

/** The item after a count: on hand as counted, available what is left once the blocked units are set aside. */
export function afterCount(item: CatalogueItem, onHand: number): CatalogueItem {
  return { ...item, onHand, available: Math.max(0, onHand - blockedUnits(item)), countNeeded: false };
}
