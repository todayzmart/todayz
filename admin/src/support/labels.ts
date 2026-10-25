import type { TicketCategory } from '@todayz/api-client/gen/model/ticketCategory';
import type { TicketMessageAuthorType } from '@todayz/api-client/gen/model/ticketMessageAuthorType';
import type { TicketOpenerType } from '@todayz/api-client/gen/model/ticketOpenerType';
import type { TicketPriority } from '@todayz/api-client/gen/model/ticketPriority';
import type { TicketStatus } from '@todayz/api-client/gen/model/ticketStatus';
import type { TicketSummary } from '@todayz/api-client/gen/model/ticketSummary';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

const STATUS_TONE: Record<TicketStatus, PillTone> = { open: 'pending', waiting: 'preparing', resolved: 'completed', closed: 'cancelled' };
const PRIORITY_TONE: Record<TicketPriority, PillTone> = { normal: 'neutral', high: 'pending', urgent: 'rejected' };

const STATUS_ID: Record<TicketStatus, string> = {
  open: 'support.status.open',
  waiting: 'support.status.waiting',
  resolved: 'support.status.resolved',
  closed: 'support.status.closed',
};
const PRIORITY_ID: Record<TicketPriority, string> = {
  normal: 'support.priority.normal',
  high: 'support.priority.high',
  urgent: 'support.priority.urgent',
};
const CATEGORY_ID: Record<TicketCategory, string> = {
  order: 'support.category.order',
  credits: 'support.category.credits',
  payouts: 'support.category.payouts',
  catalogue: 'support.category.catalogue',
  account: 'support.category.account',
  verification: 'support.category.verification',
  app: 'support.category.app',
  other: 'support.category.other',
};
const OPENER_ID: Record<TicketOpenerType, string> = { customer: 'support.opener.customer', business: 'support.opener.business' };
const AUTHOR_ID: Record<TicketMessageAuthorType, string> = {
  customer: 'support.opener.customer',
  business: 'support.opener.business',
  admin: 'support.author.admin',
};

/**
 * Minutes from `now` to the ticket's due time, both from the server (serverNow), never the device
 * clock. Negative once overdue; null when nothing is due (the ticket is not open).
 */
export function minutesLeft(t: Pick<TicketSummary, 'slaDueAt'>, now: string): number | null {
  if (!t.slaDueAt) return null;
  return Math.round((Date.parse(t.slaDueAt) - Date.parse(now)) / 60_000);
}

export function useSupportLabels() {
  const intl = useIntl();
  return useMemo(
    () => ({
      status: (s: TicketStatus) => intl.formatMessage({ id: STATUS_ID[s] }),
      statusTone: (s: TicketStatus) => STATUS_TONE[s],
      priority: (p: TicketPriority) => intl.formatMessage({ id: PRIORITY_ID[p] }),
      priorityTone: (p: TicketPriority) => PRIORITY_TONE[p],
      category: (c: TicketCategory) => intl.formatMessage({ id: CATEGORY_ID[c] }),
      opener: (o: TicketOpenerType) => intl.formatMessage({ id: OPENER_ID[o] }),
      author: (a: TicketMessageAuthorType) => intl.formatMessage({ id: AUTHOR_ID[a] }),
      name: (n: string | null | undefined) => n ?? intl.formatMessage({ id: 'support.noName' }),
      /** The SLA chip's words and tone: overdue, or due in so many minutes. */
      sla: (t: Pick<TicketSummary, 'slaDueAt' | 'overdue'>, now: string): { text: string; tone: PillTone } | null => {
        const left = minutesLeft(t, now);
        if (left === null) return null;
        const span = (m: number) => intl.formatMessage({ id: 'support.sla.span' }, { h: Math.floor(m / 60), m: m % 60 });
        if (t.overdue || left < 0) {
          return { text: intl.formatMessage({ id: 'support.sla.overdue' }, { span: span(Math.max(1, -left)) }), tone: 'rejected' };
        }
        return { text: intl.formatMessage({ id: 'support.sla.due' }, { span: span(left) }), tone: 'pending' };
      },
    }),
    [intl],
  );
}
