import { createFileRoute } from '@tanstack/react-router';
import { OrdersPage } from '../../orders/OrdersPage';

export const Route = createFileRoute('/orders/')({
  component: OrdersPage,
});
