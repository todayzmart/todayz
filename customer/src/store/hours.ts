// Today's opening hours of a shop, in IST, from the shop page's `hours` (weekly keys mon…sun, holidays by date).
import type { DiscoverHours } from '@todayz/api-client/gen/model/discoverHours';
import type { DiscoverHoursRange } from '@todayz/api-client/gen/model/discoverHoursRange';

const DAYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const;
const IST_MS = 5.5 * 60 * 60 * 1000;

/** The ranges that apply on the IST day of `serverNow`: a holiday entry wins over the weekly pattern. */
export function todayRanges(hours: DiscoverHours, serverNow: string): DiscoverHoursRange[] {
  const ist = new Date(Date.parse(serverNow) + IST_MS);
  const date = ist.toISOString().slice(0, 10);
  const holiday = hours.holidays.find((h) => h.date === date);
  if (holiday) return holiday.ranges;
  return hours.weekly[DAYS[ist.getUTCDay()] ?? 'sun'] ?? [];
}

/** "09:00" → "9 am", "21:30" → "9:30 pm" (the same style as the shared clock formatter). */
export function clock(hhmm: string): string {
  const [h = 0, m = 0] = hhmm.split(':').map(Number);
  const h12 = h % 12 === 0 ? 12 : h % 12;
  const mm = m === 0 ? '' : `:${String(m).padStart(2, '0')}`;
  return `${h12}${mm} ${h < 12 ? 'am' : 'pm'}`;
}

/** `range` words one span ("9 am to 10 pm") from the app's messages. */
export function rangesLabel(ranges: readonly DiscoverHoursRange[], range: (from: string, to: string) => string): string {
  return ranges.map((r) => range(clock(r.open), clock(r.close))).join(', ');
}
