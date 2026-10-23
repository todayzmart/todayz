import { NUMBER_TAG } from './number-tag';

const cache = new Map<number, Intl.NumberFormat>();
function nf(decimals: 0 | 1): Intl.NumberFormat {
  let f = cache.get(decimals);
  if (!f) {
    f = new Intl.NumberFormat(NUMBER_TAG, {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
      numberingSystem: 'latn',
    });
    cache.set(decimals, f);
  }
  return f;
}

/**
 * Distance in km, one decimal under 10 km ("0.6 km", "2.4 km"), whole km from 10 ("12 km").
 * Input is metres. The unit stays "km" in every language, like the digits stay Western.
 * A shop closer than 50 m shows "0.1 km" rather than "0.0 km".
 */
export function formatDistance(metres: number): string {
  if (!Number.isFinite(metres) || metres < 0) throw new RangeError(`bad distance ${metres}`);
  const tenths = Math.max(1, Math.round(metres / 100)); // tenths of a km, half up
  if (tenths >= 100) return `${nf(0).format(Math.round(metres / 1000))} km`;
  return `${nf(1).format(tenths / 10)} km`;
}
