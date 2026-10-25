import type { PayoutStatus } from '@todayz/api-client/gen/model/payoutStatus';
import { PayoutStatus as Statuses } from '@todayz/api-client/gen/model/payoutStatus';
import { text, ulid } from '../app/search';

export const STATUSES = Object.values(Statuses) as readonly PayoutStatus[];

/** A-11 filters, kept in the URL: the month, a payout status, and the payout the side panel shows. */
export interface PayoutsSearch {
  /** YYYY-MM, an IST calendar month. */
  period?: string;
  status?: PayoutStatus;
  payout?: string;
}

export const PERIOD = /^[0-9]{4}-(0[1-9]|1[0-2])$/;

export function validatePayoutsSearch(raw: Record<string, unknown>): PayoutsSearch {
  const out: PayoutsSearch = {};
  const p = text(raw.period);
  if (PERIOD.test(p)) out.period = p;
  const s = text(raw.status);
  if ((STATUSES as readonly string[]).includes(s)) out.status = s as PayoutStatus;
  const id = ulid(raw.payout);
  if (id) out.payout = id;
  return out;
}

/**
 * The last IST month that has ended, the one the 1st-of-month close writes. Only the picker's
 * starting value: the statements themselves come from the server.
 */
export function lastClosedPeriod(now: Date): string {
  const ist = new Date(now.getTime() + 330 * 60_000);
  const month = ist.getUTCMonth(); // this month, 0-based, is last month 1-based
  const year = month === 0 ? ist.getUTCFullYear() - 1 : ist.getUTCFullYear();
  return `${year}-${String(month === 0 ? 12 : month).padStart(2, '0')}`;
}
