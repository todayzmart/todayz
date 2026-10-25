import type { CreditAdjustmentStatus } from '@todayz/api-client/gen/model/creditAdjustmentStatus';
import { CreditAdjustmentStatus as Statuses } from '@todayz/api-client/gen/model/creditAdjustmentStatus';
import { text, ulid } from '../app/search';

export const STATUSES = Object.values(Statuses) as readonly CreditAdjustmentStatus[];

export type CreditsTab = 'ledger' | 'approvals';

/**
 * A-09 state kept in the URL: the tab, the ledger account the side panel shows, and the approvals
 * filter (`all`, or a status; pending when absent). The search text is not here: it can be a phone
 * number, so it stays in component state, never in the URL, history or a query key.
 */
export interface CreditsSearch {
  tab?: 'approvals';
  account?: string;
  status?: CreditAdjustmentStatus | 'all';
}

export function validateCreditsSearch(raw: Record<string, unknown>): CreditsSearch {
  const out: CreditsSearch = {};
  if (text(raw.tab) === 'approvals') out.tab = 'approvals';
  const id = ulid(raw.account);
  if (id) out.account = id;
  const s = text(raw.status);
  if (s === 'all' || (STATUSES as readonly string[]).includes(s)) out.status = s as CreditAdjustmentStatus | 'all';
  return out;
}

/** The status the approvals list asks for: pending by default, nothing for all. */
export const statusFilter = (s: CreditsSearch['status']): CreditAdjustmentStatus | undefined =>
  s === 'all' ? undefined : (s ?? 'pending');

/** Whole credits as typed (1 to 1,000,000), the contract's ±100,000,000 paise. */
export const CREDITS = /^[1-9][0-9]{0,6}$/;
export const MAX_CREDITS = 1_000_000;
