import { createFileRoute } from '@tanstack/react-router';
import { OrdersPage } from '../../orders/OrdersPage';

/**
 * B-04 Orders. A static route outranks `/$section`, so this replaces the placeholder. `?tab=asks`
 * opens the Asks tab (B-20): the target of the "New ask" push and inbox link (W3-16).
 */
export const Route = createFileRoute('/orders/')({
  validateSearch: (s: Record<string, unknown>): { tab?: 'asks' } => (s.tab === 'asks' ? { tab: 'asks' } : {}),
  component: RouteComponent,
});

function RouteComponent() {
  return <OrdersPage initialTab={Route.useSearch().tab} />;
}
