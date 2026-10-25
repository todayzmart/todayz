import type { AdminSettingUnit } from '@todayz/api-client/gen/model/adminSettingUnit';

/** The contract bound on a text setting (AdminSettingValue). */
export const MAX_TEXT = 200;

export type SettingValue = number | boolean | string;

/** `min_client_version*` keys hold a release number; the server refuses anything else. */
const SEMVER = /^\d+\.\d+\.\d+$/;
const WHOLE = /^-?\d+$/;
/** Rupees or percent with at most two decimals ("12", "12.5", "12.50"). */
const HUNDREDTHS = /^(-?)(\d+)(?:\.(\d{1,2}))?$/;

/** The unit is shown in rupees (paise) or percent (basis points), each to two decimals. */
export const inHundredths = (unit: AdminSettingUnit) => unit === 'paise' || unit === 'bp';

/** "12.50" -> 1250, exactly (string arithmetic, never floats). */
function hundredths(raw: string): number | null {
  const m = HUNDREDTHS.exec(raw);
  if (!m) return null;
  const [, sign, whole = '', frac = ''] = m;
  const n = Number(whole) * 100 + Number(frac.padEnd(2, '0'));
  if (!Number.isSafeInteger(n)) return null;
  return sign ? -n : n;
}

/** 1250 -> "12.50", 1200 -> "12"; the edit field's starting text. */
function fromHundredths(n: number): string {
  const sign = n < 0 ? '-' : '';
  const abs = Math.abs(n);
  const frac = abs % 100;
  return `${sign}${Math.trunc(abs / 100)}${frac === 0 ? '' : `.${String(frac).padStart(2, '0')}`}`;
}

/** What the edit field starts with for the stored value. */
export function toInput(unit: AdminSettingUnit, v: unknown): string {
  if (typeof v === 'number') return inHundredths(unit) ? fromHundredths(v) : String(v);
  if (typeof v === 'string') return v;
  return '';
}

export type ParseError = 'number' | 'range' | 'text' | 'version';

export interface Bounds {
  min: number | null;
  max: number | null;
}

/**
 * The edit field's text as the value the API takes, or why it cannot be. The server checks the
 * same rules (whole number within min and max, text 1–200 characters, a release number for
 * client versions); this only saves a round trip.
 */
export function parseInput(key: string, unit: AdminSettingUnit, raw: string, bounds: Bounds): { value: SettingValue } | { error: ParseError } {
  const text = raw.trim();
  if (unit === 'bool') return { value: text === 'true' };
  if (unit === 'text') {
    if (text.length === 0 || [...text].length > MAX_TEXT) return { error: 'text' };
    if (key.startsWith('min_client_version') && !SEMVER.test(text)) return { error: 'version' };
    return { value: text };
  }
  const n = inHundredths(unit) ? hundredths(text) : WHOLE.test(text) ? Number(text) : null;
  if (n === null || !Number.isSafeInteger(n)) return { error: 'number' };
  if ((bounds.min !== null && n < bounds.min) || (bounds.max !== null && n > bounds.max)) return { error: 'range' };
  return { value: n };
}
