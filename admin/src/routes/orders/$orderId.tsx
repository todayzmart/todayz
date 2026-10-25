import { createFileRoute } from '@tanstack/react-router';
import { OrderPage } from '../../orders/OrderPage';

export const Route = createFileRoute('/orders/$orderId')({ component: Order });

function Order() {
  const { orderId } = Route.useParams();
  return <OrderPage key={orderId} id={orderId} />;
}
