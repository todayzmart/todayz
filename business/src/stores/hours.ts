import type { HoursDay } from '@todayz/api-client/gen/model/hoursDay';
import type { StoreHours } from '@todayz/api-client/gen/model/storeHours';

export const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
export type Day = (typeof DAYS)[number];

/** At most four ranges a day and sixty holidays (the contract's limits). */
export const MAX_RANGES = 4;
export const MAX_HOLIDAYS = 60;

export const ALL_DAY: HoursDay = [{ open: '00:00', close: '24:00' }];

export const EMPTY_HOURS: StoreHours = { weekly: {}, holidays: [] };

const TIME = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

const minutes = (t: string): number => {
  const [h, m] = t.split(':');
  return Number(h) * 60 + Number(m);
};

/** The day's ranges, or none when the shop is closed that day. */
export const dayOf = (h: StoreHours, d: Day): HoursDay => h.weekly[d] ?? [];

export const isAllDay = (ranges: HoursDay): boolean =>
  ranges.length === 1 && ranges[0]?.open === '00:00' && ranges[0]?.close === '24:00';

/** `00:00` as a closing time means midnight, which the contract writes `24:00`. */
export function normalizeDay(ranges: HoursDay): HoursDay {
  return ranges.map((r) => (r.close === '00:00' && r.open !== '00:00' ? { ...r, close: '24:00' } : r));
}

export type DayError = 'time' | 'order' | 'overlap' | 'tooMany';

/** One day's ranges are valid when each opens before it closes, none overlap, and there are at most four. */
export function validateDay(ranges: HoursDay): DayError | null {
  if (ranges.length > MAX_RANGES) return 'tooMany';
  const list = normalizeDay(ranges);
  for (const r of list) {
    if (!TIME.test(r.open) || !(TIME.test(r.close) || r.close === '24:00')) return 'time';
    if (minutes(r.open) >= minutes(r.close)) return 'order';
  }
  const sorted = [...list].sort((a, b) => minutes(a.open) - minutes(b.open));
  for (let i = 1; i < sorted.length; i++) {
    const prev = sorted[i - 1];
    const next = sorted[i];
    if (prev && next && minutes(next.open) < minutes(prev.close)) return 'overlap';
  }
  return null;
}

export type HoursErrors = Partial<Record<Day, DayError>> & { holidays?: Record<number, DayError | 'date' | 'duplicate'> };

const DATE = /^\d{4}-\d{2}-\d{2}$/;

export function validateHours(h: StoreHours): HoursErrors {
  const out: HoursErrors = {};
  for (const d of DAYS) {
    const e = validateDay(dayOf(h, d));
    if (e) out[d] = e;
  }
  const holidays: Record<number, DayError | 'date' | 'duplicate'> = {};
  const seen = new Set<string>();
  h.holidays.forEach((x, i) => {
    if (!DATE.test(x.date) || Number.isNaN(Date.parse(`${x.date}T00:00:00Z`))) holidays[i] = 'date';
    else if (seen.has(x.date)) holidays[i] = 'duplicate';
    else {
      const e = validateDay(x.ranges);
      if (e) holidays[i] = e;
    }
    seen.add(x.date);
  });
  if (Object.keys(holidays).length > 0) out.holidays = holidays;
  return out;
}

export const hoursValid = (e: HoursErrors): boolean => Object.keys(e).length === 0;

/** What to send: closed days are left out, closing times normalised, holidays in date order. */
export function hoursPayload(h: StoreHours): StoreHours {
  const weekly: StoreHours['weekly'] = {};
  for (const d of DAYS) {
    const ranges = normalizeDay(dayOf(h, d));
    if (ranges.length > 0) weekly[d] = ranges;
  }
  const holidays = h.holidays
    .map((x) => ({ date: x.date, ranges: normalizeDay(x.ranges) }))
    .sort((a, b) => a.date.localeCompare(b.date));
  return { weekly, holidays };
}

export const sameHours = (a: StoreHours, b: StoreHours): boolean =>
  JSON.stringify(hoursPayload(a)) === JSON.stringify(hoursPayload(b));

/** Monday's hours on every day (the "same every day" shortcut). */
export function copyFirstDay(h: StoreHours, from: Day = 'mon'): StoreHours {
  const src = dayOf(h, from);
  const weekly: StoreHours['weekly'] = {};
  for (const d of DAYS) if (src.length > 0) weekly[d] = src.map((r) => ({ ...r }));
  return { ...h, weekly };
}

/** Sensible first hours for a new shop: 9 to 9, every day. */
export function defaultHours(): StoreHours {
  return copyFirstDay({ weekly: { mon: [{ open: '09:00', close: '21:00' }] }, holidays: [] });
}
