import type { AdminBusinessStatus } from '@todayz/api-client/gen/model/adminBusinessStatus';
import type { AdminLeadStatus } from '@todayz/api-client/gen/model/adminLeadStatus';
import { text } from '../app/search';

/** The A-03 queue tabs (06-screens.md), in order; Submitted is the default. */
export const QUEUES = ['submitted', 'needs_info', 'verified', 'suspended', 'rejected'] as const satisfies readonly AdminBusinessStatus[];
export const LEAD_STATUSES = ['new', 'contacted', 'closed'] as const satisfies readonly AdminLeadStatus[];

export type Queue = (typeof QUEUES)[number];

/** A-03 view, kept in the URL: the queue tab, or the Leads tab and its status filter. */
export interface BusinessesSearch {
  /** Absent is `submitted`. */
  status?: Queue;
  tab?: 'leads';
  /** Leads filter; absent is every status. */
  lead?: AdminLeadStatus;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateBusinessesSearch(raw: Record<string, unknown>): BusinessesSearch {
  const out: BusinessesSearch = {};
  const s = text(raw.status);
  if ((QUEUES as readonly string[]).includes(s)) out.status = s as Queue;
  if (text(raw.tab) === 'leads') out.tab = 'leads';
  const l = text(raw.lead);
  if ((LEAD_STATUSES as readonly string[]).includes(l)) out.lead = l as AdminLeadStatus;
  return out;
}
