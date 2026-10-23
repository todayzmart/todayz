import { createFileRoute } from '@tanstack/react-router';
import { OrderDetail } from '../../orders/OrderDetail';

function OrderDetailRoute() {
  const { orderId } = Route.useParams();
  return <OrderDetail orderId={orderId} />;
}

/** B-04 order detail. */
export const Route = createFileRoute('/orders/$orderId')({
  component: OrderDetailRoute,
});
