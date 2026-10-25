import { createFileRoute } from '@tanstack/react-router';
import { NewTicketPage } from '../../help/NewTicketPage';

/** B-15 new ticket. */
export const Route = createFileRoute('/help/new')({
  component: NewTicketPage,
});
