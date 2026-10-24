import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import type { StockLevel } from '@todayz/ui';

/** Quantities are whole units, at most this many (04 §catalogue, 01 §stock). */
export const MAX_QUANTITY = 1_000_000;

/** A price field in rupees may not be larger than this (paise stay in `Number.MAX_SAFE_INTEGER`). */
const MAX_RUPEES = 10_000_000;

type Named = Pick<StoreItem, 'name' | 'variant' | 'packSize' | 'unit'>;

/**
 * "Amul Butter · 100 g": name, variant, then pack. Shops often type the pack into the name
 * ("Atta 5kg"); then the pack is not repeated.
 */
export function itemTitle(i: Named): string {
  const pack = `${i.packSize} ${i.unit}`;
  const squash = (s: string) => s.replace(/\s+/g, '').toLowerCase();
  const named = squash(i.name).endsWith(squash(pack));
  return [i.name, i.variant, named ? null : pack].filter(Boolean).join(' · ');
}

/** Out when nothing is available to sell, low at or below the shop's threshold, else in stock. */
export function stockLevel(i: Pick<StoreItem, 'available' | 'lowStock'>): StockLevel {
  if (i.available <= 0) return 'out';
  return i.lowStock ? 'low' : 'in';
}

/** A whole number from 0 to `MAX_QUANTITY`, or null when the text is anything else. */
export function parseQuantity(text: string): number | null {
  const t = text.trim();
  if (!/^\d{1,7}$/.test(t)) return null;
  const n = Number(t);
  return n <= MAX_QUANTITY ? n : null;
}

/** "58", "58.5" or "58.50" in rupees as integer paise; null for empty, negative, zero or more than two decimals. */
export function parseRupees(text: string): number | null {
  const m = /^(\d{1,8})(?:\.(\d{1,2}))?$/.exec(text.trim());
  if (!m) return null;
  const rupees = Number(m[1]);
  if (rupees > MAX_RUPEES) return null;
  const paise = rupees * 100 + Number((m[2] ?? '').padEnd(2, '0') || 0);
  return paise > 0 ? paise : null;
}

/** The text a rupee field starts with: "58" or "58.50". */
export function rupeesText(paise: number): string {
  const rupees = Math.floor(paise / 100);
  const rest = paise % 100;
  return rest === 0 ? String(rupees) : `${rupees}.${String(rest).padStart(2, '0')}`;
}

/** The MRP a price is checked against: the shop's own, else the master product's. */
export function effectiveMrp(i: Pick<StoreItem, 'mrpPaise' | 'productMrpPaise'>): number | null {
  return i.mrpPaise ?? i.productMrpPaise;
}
