/** Parsing for the catalogue forms, kept apart from the dialogs so it is tested on its own. */

/** The contract's bounds (AdminProductCreate / AdminProductUpdate). */
export const MAX_PACK = 1_000_000;
export const MAX_MRP_PAISE = 100_000_000_000;
export const MAX_BARCODES = 10;
const BARCODE = /^[0-9]{8,14}$/;
const RUPEES = /^\d{1,9}(\.\d{1,2})?$/;
const SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export type Parsed<T> = { value: T } | { error: string };

/** A pack size such as "1" or "0.5"; positive, at most the contract's bound. */
export function parsePack(raw: string): Parsed<number> {
  const t = raw.trim();
  const n = Number(t);
  if (t === '' || !Number.isFinite(n) || n <= 0 || n > MAX_PACK) return { error: 'pack' };
  return { value: n };
}

/** An MRP typed in rupees ("189" or "189.50") as paise; empty is no MRP. */
export function parseMrp(raw: string): Parsed<number | null> {
  const t = raw.trim().replace(/,/g, '');
  if (t === '') return { value: null };
  if (!RUPEES.test(t)) return { error: 'mrp' };
  const [r = '0', p = ''] = t.split('.');
  const paise = Number(r) * 100 + Number(p.padEnd(2, '0'));
  if (paise < 1 || paise > MAX_MRP_PAISE) return { error: 'mrp' };
  return { value: paise };
}

/** Barcodes separated by commas, spaces or new lines; each 8 to 14 digits, no repeats, at most 10. */
export function parseBarcodes(raw: string): Parsed<string[]> {
  const codes = raw.split(/[\s,]+/).filter(Boolean);
  if (codes.some((c) => !BARCODE.test(c))) return { error: 'barcode' };
  const unique = [...new Set(codes)];
  if (unique.length > MAX_BARCODES) return { error: 'barcodes' };
  return { value: unique };
}

/** A slug or a Lucide icon name: lower-case words joined by single hyphens. */
export const isSlug = (v: string) => v.length <= 64 && SLUG.test(v);

/** Paise back to the rupees a person types ("189.50"). */
export const rupees = (paise: number | null) => (paise === null ? '' : (paise / 100).toFixed(paise % 100 === 0 ? 0 : 2));
