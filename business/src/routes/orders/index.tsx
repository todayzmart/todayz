import { createFileRoute } from '@tanstack/react-router';
import { OrdersPage } from '../../orders/OrdersPage';

/** B-04 Orders. A static route outranks `/$section`, so this replaces the placeholder. */
export const Route = createFileRoute('/orders/')({
  component: OrdersPage,
});
