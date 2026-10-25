// B-20 times. India has one fixed offset (+05:30, no daylight saving), so "today" and "tomorrow" for
// the ready-by time and the done list are worked out from the server's clock in IST, never the device's.
import type { StoreAsk } from '@todayz/api-client/gen/model/storeAsk';
import type { StoreAskNeedBy } from '@todayz/api-client/gen/model/storeAskNeedBy';

const IST_MS = 330 * 60_000;
const DAY_MS = 86_400_000;

/** The IST calendar day of an instant, as a day number (days since 1970-01-01 IST). */
export function istDay(at: string | number): number {
  const ms = typeof at === 'number' ? at : Date.parse(at);
  return Math.floor((ms + IST_MS) / DAY_MS);
}

export type ReadyDay = 'today' | 'tomorrow';

/** The days a ready-by time may fall on (01 §11.2): today, and tomorrow only when the customer needs it tomorrow. */
export function readyDays(needBy: StoreAskNeedBy): ReadyDay[] {
  return needBy === 'tomorrow' ? ['today', 'tomorrow'] : ['today'];
}

/** "HH:MM" on that IST day as an RFC 3339 instant, or null when the text is not a time. */
export function readyByIso(nowMs: number, day: ReadyDay, hhmm: string): string | null {
  const m = /^(\d{2}):(\d{2})$/.exec(hhmm);
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  const start = (istDay(nowMs) + (day === 'tomorrow' ? 1 : 0)) * DAY_MS - IST_MS;
  return new Date(start + (h * 60 + min) * 60_000).toISOString();
}

/** A ready-by time that has already passed (by the server's clock) cannot be sent. */
export function readyByPast(iso: string, nowMs: number): boolean {
  return Date.parse(iso) < nowMs - 60_000;
}

/** Done for the day: replied or missed, and sent today (IST). Older ones sit under "Earlier". */
export function doneToday(ask: StoreAsk, serverNow: string): boolean {
  return istDay(ask.sentAt) === istDay(serverNow);
}
