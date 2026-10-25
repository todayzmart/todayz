import { createFileRoute } from '@tanstack/react-router';
import { NewTicketPage } from '../../help/NewTicketPage';

const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/;

function NewRoute() {
  const { orderId } = Route.useSearch();
  return <NewTicketPage {...(orderId ? { orderId } : {})} />;
}

/** C-19 new ticket; `?orderId=` comes from "Get help with this order" on C-12. */
export const Route = createFileRoute('/help/new')({
  validateSearch: (s: Record<string, unknown>): { orderId?: string } =>
    typeof s.orderId === 'string' && ULID.test(s.orderId) ? { orderId: s.orderId } : {},
  component: NewRoute,
});
