import { createFileRoute } from '@tanstack/react-router';
import { TicketPage } from '../../help/TicketPage';

function TicketRoute() {
  const { ticketId } = Route.useParams();
  return <TicketPage ticketId={ticketId} />;
}

/** B-15 ticket thread. */
export const Route = createFileRoute('/help/$ticketId')({
  component: TicketRoute,
});
