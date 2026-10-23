import { createFileRoute } from '@tanstack/react-router';
import { OrderPage } from '../../orders/OrderPage';

export const Route = createFileRoute('/orders/$orderId')({
  validateSearch: (search: Record<string, unknown>): { placed?: boolean } => (search.placed === 1 || search.placed === '1' || search.placed === true ? { placed: true } : {}),
  component: OrderRoute,
});

function OrderRoute() {
  const { orderId } = Route.useParams();
  const { placed } = Route.useSearch();
  return <OrderPage orderId={orderId} placed={placed === true} />;
}
