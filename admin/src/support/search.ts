import type { ListAdminTicketsAssignee } from '@todayz/api-client/gen/model/listAdminTicketsAssignee';
import { ListAdminTicketsAssignee as Assignees } from '@todayz/api-client/gen/model/listAdminTicketsAssignee';
import type { ListAdminTicketsParams } from '@todayz/api-client/gen/model/listAdminTicketsParams';
import type { TicketOpenerType } from '@todayz/api-client/gen/model/ticketOpenerType';
import { TicketOpenerType as Openers } from '@todayz/api-client/gen/model/ticketOpenerType';
import type { TicketStatus } from '@todayz/api-client/gen/model/ticketStatus';
import { TicketStatus as Statuses } from '@todayz/api-client/gen/model/ticketStatus';
import { text } from '../app/search';

export const STATUSES = Object.values(Statuses) as readonly TicketStatus[];
export const ASSIGNEES = Object.values(Assignees) as readonly ListAdminTicketsAssignee[];
export const OPENERS = Object.values(Openers) as readonly TicketOpenerType[];

/** A-14 filters, kept in the URL: the status tab (open when absent), whose tickets, and who opened them. */
export interface SupportSearch {
  status?: TicketStatus;
  assignee?: ListAdminTicketsAssignee;
  opener?: TicketOpenerType;
}

const pick = <T extends string>(all: readonly T[], v: unknown): T | undefined => {
  const s = text(v);
  return (all as readonly string[]).includes(s) ? (s as T) : undefined;
};

export function validateSupportSearch(raw: Record<string, unknown>): SupportSearch {
  const out: SupportSearch = {};
  const status = pick(STATUSES, raw.status);
  if (status && status !== 'open') out.status = status;
  const assignee = pick(ASSIGNEES, raw.assignee);
  if (assignee) out.assignee = assignee;
  const opener = pick(OPENERS, raw.opener);
  if (opener) out.opener = opener;
  return out;
}

/** The list call's filters; the page size and cursor are added by the query. */
export function ticketsParams(s: SupportSearch): Omit<ListAdminTicketsParams, 'limit' | 'cursor'> {
  return { status: s.status ?? 'open', ...(s.assignee ? { assignee: s.assignee } : {}), ...(s.opener ? { opener: s.opener } : {}) };
}
