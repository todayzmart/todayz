import type { CustomerSegment } from '@todayz/api-client/gen/model/customerSegment';

export interface CustomersSearch {
  segment?: CustomerSegment;
}

/** `/customers?segment=all|top|slipping`; anything else is dropped. */
export function validateCustomersSearch(raw: Record<string, unknown>): CustomersSearch {
  const s = raw.segment;
  return s === 'all' || s === 'top' || s === 'slipping' ? { segment: s } : {};
}
