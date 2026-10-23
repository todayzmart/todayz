import { describe, expect, it } from 'vitest';
import {
  LOCALES,
  PSEUDO_LOCALE,
  createFormatters,
  formatAbsolute,
  formatClock,
  formatDistance,
  formatMoney,
  formatRelative,
  formatTime,
  roundToRupee,
  type MessageLocale,
  type MoneyStyle,
} from '../src/index';

const ALL: MessageLocale[] = [...LOCALES, PSEUDO_LOCALE];

describe('SC-I18N-02 formatMoney: ₹, Indian grouping, paise rules', () => {
  // paise, style, want
  const table: [number, MoneyStyle, string][] = [
    [0, 'product', '₹0'],
    [0, 'receipt', '₹0.00'],
    [-0, 'dashboard', '₹0'],
    [100, 'product', '₹1'],
    [28900, 'product', '₹289'],
    [28950, 'product', '₹289.50'], // paise matter: shown
    [28905, 'dashboard', '₹289.05'],
    [99, 'product', '₹0.99'],
    [1, 'product', '₹0.01'],
    [99999, 'dashboard', '₹999.99'],
    [100000, 'dashboard', '₹1,000'],
    [9999900, 'dashboard', '₹99,999'],
    [12450000, 'dashboard', '₹1,24,500'], // lakh grouping
    [124600, 'receipt', '₹1,246.00'], // POS receipt: always two decimals
    [124600, 'product', '₹1,246'],
    [10000000, 'dashboard', '₹1,00,000'], // 1 lakh
    [999999900, 'dashboard', '₹99,99,999'],
    [1000000000, 'dashboard', '₹1,00,00,000'], // 1 crore
    [12345678900, 'dashboard', '₹12,34,56,789'],
    [123456789012, 'receipt', '₹1,23,45,67,890.12'],
    [-28900, 'product', '-₹289'],
    [-12450000, 'dashboard', '-₹1,24,500'],
    [-1000000000, 'dashboard', '-₹1,00,00,000'],
    [-124600, 'receipt', '-₹1,246.00'],
    [-50, 'product', '-₹0.50'],
  ];

  it.each(table)('%i paise as %s -> %s', (paise, style, want) => {
    expect(formatMoney(paise, style)).toBe(want);
  });

  it('defaults to the product style', () => {
    expect(formatMoney(28900)).toBe('₹289');
  });

  it('rejects fractional paise', () => {
    expect(() => formatMoney(10.5)).toThrow(RangeError);
    expect(() => formatMoney(Number.NaN)).toThrow(RangeError);
  });

  it.each(ALL)('is identical in %s: Indian grouping, Latin digits, ₹ sign', (locale) => {
    const f = createFormatters(locale);
    expect(f.money(12450000, 'dashboard')).toBe('₹1,24,500');
    expect(f.money(1000000000, 'dashboard')).toBe('₹1,00,00,000');
    expect(f.money(124600, 'receipt')).toBe('₹1,246.00');
  });
});

describe('roundToRupee (POS round-off, half rounds up)', () => {
  it.each([
    [0, 0],
    [10000, 10000],
    [10049, 10000],
    [10050, 10100], // ₹100.50 -> ₹101
    [10051, 10100],
    [10099, 10100],
    [49, 0],
    [50, 100],
    [-50, -100],
    [-49, 0],
    [-10050, -10100],
    [124649, 124600],
  ])('%i -> %i', (paise, want) => {
    expect(roundToRupee(paise)).toBe(want);
  });
});

describe('SC-I18N-03 formatDistance: km, one decimal under 10 km', () => {
  it.each([
    [0, '0.1 km'], // never "0.0 km"
    [30, '0.1 km'],
    [49, '0.1 km'],
    [100, '0.1 km'],
    [550, '0.6 km'],
    [600, '0.6 km'],
    [1000, '1.0 km'],
    [2400, '2.4 km'],
    [2449, '2.4 km'],
    [2450, '2.5 km'],
    [9949, '9.9 km'],
    [9950, '10 km'], // rounds up to 10: no decimal from here
    [10000, '10 km'],
    [12400, '12 km'],
    [12600, '13 km'],
    [20000, '20 km'],
    [150000, '150 km'],
  ])('%i m -> %s', (metres, want) => {
    expect(formatDistance(metres)).toBe(want);
  });

  it('rejects negative or non-finite distances', () => {
    expect(() => formatDistance(-1)).toThrow(RangeError);
    expect(() => formatDistance(Number.POSITIVE_INFINITY)).toThrow(RangeError);
  });

  it.each(ALL)('is the same in %s', (locale) => {
    expect(createFormatters(locale).distance(600)).toBe('0.6 km');
  });
});

// 2026-10-23 18:00 IST
const NOW = Date.parse('2026-10-23T12:30:00Z');
const ago = (ms: number) => NOW - ms;
const MIN = 60_000;
const HOUR = 60 * MIN;

describe('SC-I18N-03 formatRelative (English)', () => {
  it.each([
    [0, 'now'],
    [30_000, 'now'],
    [59_999, 'now'],
    [MIN, '1 min ago'],
    [12 * MIN, '12 min ago'],
    [59 * MIN + 59_000, '59 min ago'],
    [HOUR, '1 hr ago'],
    [3 * HOUR + 30 * MIN, '3 hr ago'],
    [23 * HOUR + 59 * MIN, '23 hr ago'],
    [24 * HOUR, '1 day ago'],
    [-5 * MIN, 'now'], // clock skew: the time is in the future
  ])('%i ms ago -> %s', (ms, want) => {
    expect(formatRelative(ago(ms), NOW, 'en')).toBe(want);
  });

  it('accepts Date, number and ISO string', () => {
    expect(formatRelative(new Date(ago(12 * MIN)), new Date(NOW), 'en')).toBe('12 min ago');
    expect(formatRelative(new Date(ago(12 * MIN)).toISOString(), NOW, 'en')).toBe('12 min ago');
  });

  it('rejects an invalid date', () => {
    expect(() => formatRelative('not a date', NOW)).toThrow(RangeError);
  });
});

describe('SC-I18N-03 formatAbsolute in IST (English)', () => {
  it.each([
    ['2026-10-23T12:30:00Z', 'Today, 6:00 pm'],
    ['2026-10-23T00:00:00Z', 'Today, 5:30 am'],
    ['2026-10-22T18:30:00Z', 'Today, 12:00 am'], // midnight IST starts the IST day, not midnight UTC
    ['2026-10-22T18:29:59Z', 'Yesterday, 11:59 pm'],
    ['2026-10-22T13:10:00Z', 'Yesterday, 6:40 pm'],
    ['2026-10-22T06:30:00Z', 'Yesterday, 12:00 pm'],
    ['2026-10-21T13:10:00Z', '21 Oct, 6:40 pm'],
    ['2026-01-05T04:35:00Z', '5 Jan, 10:05 am'],
    ['2025-12-31T18:29:00Z', '31 Dec 2025, 11:59 pm'], // previous year shows the year
    ['2025-12-31T18:30:00Z', '1 Jan, 12:00 am'], // 1 Jan 2026 IST: this year
  ])('%s -> %s', (iso, want) => {
    expect(formatAbsolute(iso, NOW, 'en')).toBe(want);
  });

  it('writes September as Sep, never Sept', () => {
    expect(formatAbsolute('2026-09-22T13:10:00Z', NOW, 'en')).toBe('22 Sep, 6:40 pm');
    expect(formatAbsolute('2025-09-22T13:10:00Z', NOW, 'en')).toBe('22 Sep 2025, 6:40 pm');
  });

  it('rejects an ISO string without Z or an offset (it would be read in the device zone)', () => {
    expect(() => formatAbsolute('2026-10-22T18:40:00', NOW, 'en')).toThrow(RangeError);
    expect(() => formatClock('2026-10-22T18:40:00')).toThrow(RangeError);
    expect(() => formatRelative('2026-10-22T18:40:00', NOW, 'en')).toThrow(/time zone/);
    expect(() => formatTime('2026-10-22', NOW, 'en', { relativeWithinMs: MIN })).toThrow(RangeError);
    expect(formatClock('2026-10-22T18:40:00+05:30')).toBe('6:40 pm');
    expect(formatClock('2026-10-22T13:10:00.000Z')).toBe('6:40 pm');
  });

  it('is not shifted by the device time zone: uses IST only', () => {
    // 18:30 UTC on the 22nd is already the 23rd in India.
    expect(formatClock('2026-10-22T18:30:00Z')).toBe('12:00 am');
    expect(formatClock('2026-10-22T06:29:00Z')).toBe('11:59 am');
    expect(formatClock('2026-10-22T06:30:00Z')).toBe('12:00 pm');
  });
});

describe('SC-I18N-03 formatTime: relative while fresh, absolute once old', () => {
  const opts = { relativeWithinMs: 60 * MIN };
  it('switches at the caller-supplied cut-off', () => {
    expect(formatTime(ago(12 * MIN), NOW, 'en', opts)).toBe('12 min ago');
    expect(formatTime(ago(60 * MIN - 1), NOW, 'en', opts)).toBe('59 min ago');
    expect(formatTime(ago(60 * MIN), NOW, 'en', opts)).toBe('Today, 5:00 pm');
    expect(formatTime(Date.parse('2026-10-22T13:10:00Z'), NOW, 'en', opts)).toBe('Yesterday, 6:40 pm');
  });
});

describe('SC-I18N-03 times in every language: Latin digits, IST, 12-hour with am/pm', () => {
  it.each(LOCALES)('%s', (locale) => {
    const yest = formatAbsolute('2026-10-22T13:10:00Z', NOW, locale);
    expect(yest.endsWith(', 6:40 pm')).toBe(true);
    const old = formatAbsolute('2026-10-20T13:10:00Z', NOW, locale);
    expect(old).toMatch(/^20 \S.*, 6:40 pm$/u);
    const rel = formatRelative(ago(12 * MIN), NOW, locale);
    expect(rel).toContain('12');
    for (const s of [yest, old, rel]) expect([...s].filter((c) => /\p{Nd}/u.test(c) && !/[0-9]/.test(c))).toEqual([]); // nu-latn: Latin digits only
  });

  it('uses the language for words and month names', () => {
    expect(formatAbsolute('2026-10-20T13:10:00Z', NOW, 'kn')).toContain('ಅಕ್ಟೋಬರ್'); // localization.md: "22 ಅಕ್ಟೋಬರ್, 6:40 pm"
    expect(formatAbsolute('2026-10-22T13:10:00Z', NOW, 'hi')).not.toContain('Yesterday');
    expect(formatRelative(ago(12 * MIN), NOW, 'hi')).not.toBe('12 min ago');
  });
});
