import type { AdminOrderStatus } from '@todayz/api-client/gen/model/adminOrderStatus';
import type { ListAdminOrdersParams } from '@todayz/api-client/gen/model/listAdminOrdersParams';
import { day, dayEnd, dayStart, text, ulid } from '../app/search';
import { STATUSES } from './labels';

/** A-07 filters, kept in the URL so a search can be shared and survives a reload. */
export interface OrdersSearch {
  number?: string;
  status?: AdminOrderStatus;
  storeId?: string;
  customerId?: string;
  /** IST calendar days, YYYY-MM-DD. */
  from?: string;
  to?: string;
}

const NUMBER = /^[A-Za-z0-9-]{1,20}$/;

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateOrdersSearch(raw: Record<string, unknown>): OrdersSearch {
  const out: OrdersSearch = {};
  const n = text(raw.number);
  if (NUMBER.test(n)) out.number = n;
  const s = text(raw.status);
  if ((STATUSES as readonly string[]).includes(s)) out.status = s as AdminOrderStatus;
  for (const k of ['storeId', 'customerId'] as const) {
    const id = ulid(raw[k]);
    if (id) out[k] = id;
  }
  for (const k of ['from', 'to'] as const) {
    const d = day(raw[k]);
    if (d) out[k] = d;
  }
  return out;
}

/** The API's parameters: a day runs from IST midnight to the next IST midnight. */
export function ordersParams(s: OrdersSearch): Omit<ListAdminOrdersParams, 'limit' | 'cursor'> {
  const p: Omit<ListAdminOrdersParams, 'limit' | 'cursor'> = {};
  if (s.number) p.number = s.number;
  if (s.status) p.status = s.status;
  if (s.storeId) p.storeId = s.storeId;
  if (s.customerId) p.customerId = s.customerId;
  if (s.from) p.from = dayStart(s.from);
  if (s.to) p.to = dayEnd(s.to);
  return p;
}
