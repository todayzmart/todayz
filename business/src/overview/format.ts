import { istParts, type DateInput } from '@todayz/i18n';
import type { KpiDelta } from '@todayz/ui';
import type { IntlShape } from 'react-intl';

const IST = 'Asia/Kolkata';

/** "Thursday" / "Thu" for an instant, read in IST. */
export function weekday(intl: IntlShape, d: DateInput, style: 'long' | 'short' = 'long'): string {
  return intl.formatDate(new Date(d), { weekday: style, timeZone: IST });
}

/** "Thursday, 22 October", read in IST. */
export function longDate(intl: IntlShape, d: DateInput): string {
  return intl.formatDate(new Date(d), { weekday: 'long', day: 'numeric', month: 'long', timeZone: IST });
}

export type DayPart = 'morning' | 'afternoon' | 'evening';

export function dayPart(d: DateInput): DayPart {
  const { hour } = istParts(d);
  return hour < 12 ? 'morning' : hour < 17 ? 'afternoon' : 'evening';
}

const h12 = (hour: number) => (hour % 12 === 0 ? 12 : hour % 12);

/** "6 pm" for an hour of the day. */
export function hourLabel(intl: IntlShape, hour: number): string {
  return hour < 12
    ? intl.formatMessage({ id: 'overview.hour.am' }, { h: h12(hour) })
    : intl.formatMessage({ id: 'overview.hour.pm' }, { h: h12(hour) });
}

/** "6p" on the chart axis. */
export function axisHour(intl: IntlShape, hour: number): string {
  return hour < 12
    ? intl.formatMessage({ id: 'overview.chart.hourAm' }, { h: h12(hour) })
    : intl.formatMessage({ id: 'overview.chart.hourPm' }, { h: h12(hour) });
}

/** "3:42" from seconds; never negative. */
export function mmss(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/** Change against the comparison as a whole percentage, or as a count with `absolute`. */
export function delta(intl: IntlShape, now: number, prev: number, absolute = false): KpiDelta {
  const diff = absolute ? now - prev : prev === 0 ? 0 : Math.round(((now - prev) / prev) * 100);
  const direction = diff > 0 ? 'up' : diff < 0 ? 'down' : 'flat';
  const value = `${Math.abs(diff)}${absolute ? '' : '%'}`;
  const label =
    direction === 'up'
      ? intl.formatMessage({ id: 'overview.delta.up' }, { value })
      : direction === 'down'
        ? intl.formatMessage({ id: 'overview.delta.down' }, { value })
        : intl.formatMessage({ id: 'overview.delta.flat' });
  return { value, direction, label };
}
