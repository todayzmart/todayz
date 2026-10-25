import type { AdminStoreStatus } from '@todayz/api-client/gen/model/adminStoreStatus';
import { text } from '../app/search';
import { STORE_STATUSES } from './labels';

/** A-05 view, kept in the URL: the list with its status and city filters, or the review queue. */
export interface StoresSearch {
  tab?: 'review';
  /** Absent is every status. */
  status?: AdminStoreStatus;
  city?: string;
}

const CITY = /^[\p{L} .'-]{1,60}$/u;

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateStoresSearch(raw: Record<string, unknown>): StoresSearch {
  const out: StoresSearch = {};
  if (text(raw.tab) === 'review') out.tab = 'review';
  const s = text(raw.status);
  if ((STORE_STATUSES as readonly string[]).includes(s)) out.status = s as AdminStoreStatus;
  const c = text(raw.city);
  if (CITY.test(c)) out.city = c;
  return out;
}
