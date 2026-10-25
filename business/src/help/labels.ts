import type { BizTicketStatus } from '@todayz/api-client/gen/model/bizTicketStatus';
import type { PillTone } from '@todayz/ui';

/** Open is with support, waiting is the shop's turn, resolved and closed are done. */
export const STATUS_TONE: Record<BizTicketStatus, PillTone> = {
  open: 'pending',
  waiting: 'ready',
  resolved: 'completed',
  closed: 'neutral',
};
