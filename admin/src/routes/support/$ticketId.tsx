import { createFileRoute } from '@tanstack/react-router';
import { TicketPage } from '../../support/TicketPage';

export const Route = createFileRoute('/support/$ticketId')({ component: Ticket });

function Ticket() {
  const { ticketId } = Route.useParams();
  return <TicketPage key={ticketId} id={ticketId} />;
}
