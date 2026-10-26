import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import { TriageAssist } from '../ai-assist/support/TriageAssist';

/**
 * Where the triage assistant (AI-A2) sits on the ticket. W3-13 renders `admin/src/ai-assist/triage`
 * inside this mount; until then it is an empty box. The page mounts it only while
 * `ai_triage_enabled` is on (the ticket's `assistEnabled`). What the assistant suggests is a draft:
 * the agent still picks the priority, writes or edits the reply and sends it.
 */
export function AssistSlot({ detail }: { detail: TicketDetail }) {
  return (
    <div data-slot="support-assist" data-ticket={detail.ticket.id}>
      <TriageAssist key={detail.ticket.id} detail={detail} />
    </div>
  );
}
