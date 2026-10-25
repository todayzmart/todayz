import type { MyTicketStatus } from '@todayz/api-client/gen/model/myTicketStatus';
import type { PillTone } from '@todayz/ui';

/** Open is with support, waiting is the customer's turn, resolved and closed are done. */
export const STATUS_TONE: Record<MyTicketStatus, PillTone> = {
  open: 'pending',
  waiting: 'ready',
  resolved: 'completed',
  closed: 'neutral',
};
