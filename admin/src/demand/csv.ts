import type { DemandGap } from '@todayz/api-client/gen/model/demandGap';

/**
 * One CSV field. Asked words are customers' own text, so a leading `=`, `+`, `-`, `@`, tab or CR
 * gets a `'` first and a spreadsheet never reads it as a formula.
 */
export function csvField(v: string | number): string {
  let s = String(v);
  if (typeof v === 'string' && /^[=+\-@\t\r]/.test(s)) s = `'${s}`;
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** The Gaps rows as shown: no customer is named or linked, only counts (06 A-23). */
export function gapsCsv(rows: readonly DemandGap[]): string {
  const head = ['city', 'product', 'product_id', 'category', 'customers', 'asks', 'shops_asked', 'last_asked_at'];
  const lines = rows.map((g) =>
    [g.cityName, g.itemName, g.productId ?? '', g.category ?? '', g.customers, g.asks, g.shopsAsked, g.lastAskedAt].map(csvField).join(','),
  );
  return [head.join(','), ...lines].join('\n') + '\n';
}

