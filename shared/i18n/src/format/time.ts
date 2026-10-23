// Times (design README "Voice and copy", localization.md). Every time is shown in IST (Asia/Kolkata,
// UTC+5:30, no daylight saving) with a 12-hour clock, Latin digits and lowercase am/pm in every
// language: "12 min ago", "Yesterday, 6:40 pm", "22 ಅಕ್ಟೋಬರ್, 6:40 pm".
import { baseLocale, intlTag, type MessageLocale } from '../locales';

export const IST_OFFSET_MS = 5.5 * 60 * 60 * 1000;
const DAY_MS = 24 * 60 * 60 * 1000;

export type DateInput = Date | number | string;

/** An ISO 8601 string must say its zone: "Z" or an offset like "+05:30". */
const HAS_ZONE = /[T ]\d{2}(?::?\d{2}(?::?\d{2}(?:[.,]\d+)?)?)?\s*(?:Z|[+-]\d{2}(?::?\d{2})?)$/i;

/**
 * Milliseconds since the epoch. A string without "Z" or an offset (`2026-10-22T18:40:00`) throws a
 * RangeError: JavaScript would read it in the device's own time zone, so the same string would show
 * different times on different phones. The API always sends an offset.
 */
const toMs = (d: DateInput): number => {
  if (typeof d === 'string' && !HAS_ZONE.test(d.trim())) throw new RangeError(`date string has no time zone (add Z or an offset): ${d}`);
  const ms = d instanceof Date ? d.getTime() : typeof d === 'number' ? d : Date.parse(d);
  if (!Number.isFinite(ms)) throw new RangeError(`bad date ${String(d)}`);
  return ms;
};

/** Calendar parts of an instant in IST. `dayNumber` counts whole IST days since 1970-01-01. */
export function istParts(d: DateInput): { year: number; month: number; day: number; hour: number; minute: number; dayNumber: number } {
  const shifted = new Date(toMs(d) + IST_OFFSET_MS); // IST has no DST, so a fixed shift is exact
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth(),
    day: shifted.getUTCDate(),
    hour: shifted.getUTCHours(),
    minute: shifted.getUTCMinutes(),
    dayNumber: Math.floor((toMs(d) + IST_OFFSET_MS) / DAY_MS),
  };
}

/** "6:40 pm" (IST). */
export function formatClock(d: DateInput): string {
  const { hour, minute } = istParts(d);
  const h12 = hour % 12 === 0 ? 12 : hour % 12;
  return `${h12}:${String(minute).padStart(2, '0')} ${hour < 12 ? 'am' : 'pm'}`;
}

const rtfCache = new Map<string, Intl.RelativeTimeFormat>();
function rtf(locale: MessageLocale, numeric: 'always' | 'auto'): Intl.RelativeTimeFormat {
  const key = `${locale}|${numeric}`;
  let f = rtfCache.get(key);
  if (!f) {
    f = new Intl.RelativeTimeFormat(intlTag(locale), { numeric, style: 'short', numberingSystem: 'latn' } as Intl.RelativeTimeFormatOptions);
    rtfCache.set(key, f);
  }
  return f;
}

/** English short forms carry full stops ("12 min. ago"); the product copy is "12 min ago". */
const tidy = (locale: MessageLocale, s: string): string => (baseLocale(locale) === 'en' ? s.replace(/\.(?=\s|$)/g, '') : s);

/**
 * Relative time for fresh things: "now", "12 min ago", "3 hr ago", "2 d ago". A time in the future
 * (device clock a little ahead of the server) reads "now". Which times are fresh enough to show
 * relative is the caller's call: see formatTime.
 */
export function formatRelative(then: DateInput, now: DateInput, locale: MessageLocale = 'en'): string {
  const diff = Math.max(0, toMs(now) - toMs(then));
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return tidy(locale, rtf(locale, 'auto').format(0, 'second'));
  if (minutes < 60) return tidy(locale, rtf(locale, 'always').format(-minutes, 'minute'));
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return tidy(locale, rtf(locale, 'always').format(-hours, 'hour'));
  return tidy(locale, rtf(locale, 'always').format(-Math.floor(hours / 24), 'day'));
}

/** Fixed three-letter English months: Intl gives "Sept" for September in en-IN, the copy says "Sep". */
const EN_MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'] as const;

const upperFirst =(locale: MessageLocale, s: string): string => (s ? s.charAt(0).toLocaleUpperCase(intlTag(locale)) + s.slice(1) : s);

/**
 * Absolute time for old things, in IST: "Today, 6:40 pm", "Yesterday, 6:40 pm", "22 Oct, 6:40 pm",
 * and with the year when it is not the current IST year: "22 Oct 2025, 6:40 pm". Month names follow
 * the language, short in English and full in the Indic languages (localization.md).
 */
export function formatAbsolute(then: DateInput, now: DateInput, locale: MessageLocale = 'en'): string {
  const t = istParts(then);
  const n = istParts(now);
  const clock = formatClock(then);
  const delta = n.dayNumber - t.dayNumber;
  if (delta === 0 || delta === 1) {
    const word = rtf(locale, 'auto').format(-delta, 'day');
    return `${upperFirst(locale, word)}, ${clock}`;
  }
  const month =
    baseLocale(locale) === 'en'
      ? EN_MONTHS[t.month]
      : new Intl.DateTimeFormat(intlTag(locale), { month: 'long', timeZone: 'Asia/Kolkata' }).format(new Date(toMs(then)));
  const year = t.year === n.year ? '' : ` ${t.year}`;
  return `${t.day} ${month}${year}, ${clock}`;
}

export interface FormatTimeOptions {
  /**
   * How long a time counts as fresh and reads relative ("12 min ago"); older reads absolute. There
   * is no platform-wide number for this in the plan (open question for the lead), so the caller
   * must pass one, in milliseconds.
   */
  relativeWithinMs: number;
}

/** Relative while fresh, absolute once old. */
export function formatTime(then: DateInput, now: DateInput, locale: MessageLocale, opts: FormatTimeOptions): string {
  return toMs(now) - toMs(then) < opts.relativeWithinMs ? formatRelative(then, now, locale) : formatAbsolute(then, now, locale);
}
