import { createFileRoute } from '@tanstack/react-router';
import { OrdersPage } from '../../orders/OrdersPage';
import { validateOrdersSearch } from '../../orders/search';

export const Route = createFileRoute('/orders/')({ validateSearch: validateOrdersSearch, component: Orders });

function Orders() {
  return <OrdersPage search={Route.useSearch()} />;
}
