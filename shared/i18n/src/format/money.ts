// Money (01-product-rules.md, design README "Voice and copy"). Amounts are integer paise, always.
import { NUMBER_TAG } from './number-tag';

/**
 * - `dashboard`: ₹1,24,500. Whole rupees; paise appear only when the amount has them ("₹289.50").
 * - `product`: ₹289. Same rule as dashboard; named separately so call sites say what they mean.
 * - `receipt`: ₹1,246.00. Always two decimals. POS receipts and bills only.
 */
export type MoneyStyle = 'dashboard' | 'product' | 'receipt';

const cache = new Map<number, Intl.NumberFormat>();

function nf(decimals: 0 | 2): Intl.NumberFormat {
  let f = cache.get(decimals);
  if (!f) {
    f = new Intl.NumberFormat(NUMBER_TAG, {
      style: 'currency',
      currency: 'INR',
      currencyDisplay: 'symbol',
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
      numberingSystem: 'latn',
    });
    cache.set(decimals, f);
  }
  return f;
}

/** Money is never fractional paise; also folds -0 into 0. */
function checkPaise(paise: number): number {
  if (!Number.isInteger(paise)) throw new RangeError(`paise must be an integer, got ${paise}`);
  return paise === 0 ? 0 : paise;
}

export function formatMoney(paise: number, style: MoneyStyle = 'product'): string {
  const p = checkPaise(paise);
  const decimals: 0 | 2 = style === 'receipt' || p % 100 !== 0 ? 2 : 0;
  return nf(decimals).format(p / 100);
}

/**
 * POS round-off to the nearest ₹1, half rounds up (₹100.50 becomes ₹101). Reversals mirror it:
 * -₹100.50 becomes -₹101, so a reversal always undoes its sale.
 */
export function roundToRupee(paise: number): number {
  const p = checkPaise(paise);
  const rounded = Math.floor((Math.abs(p) + 50) / 100) * 100;
  return p < 0 && rounded !== 0 ? -rounded : rounded;
}
