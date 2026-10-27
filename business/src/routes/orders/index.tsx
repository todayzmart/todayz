import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { OrdersPage, type OrdersSearch } from '../../orders/OrdersPage';
import { parseSalesSearch } from '../../orders/salesQueries';

/**
 * B-04 Orders. A static route outranks `/$section`, so this replaces the placeholder. `?tab=asks`
 * opens the Asks tab (B-20): the target of the "New ask" push and inbox link (W3-16). `?view=sales`
 * opens Counter sales (D-268) with its dates, filters and open bill in the URL.
 */
export const Route = createFileRoute('/orders/')({
  validateSearch: (s: Record<string, unknown>): OrdersSearch => {
    const sales = parseSalesSearch(s);
    if (sales.view) return sales;
    return s.tab === 'asks' ? { tab: 'asks' } : {};
  },
  component: RouteComponent,
});

function RouteComponent() {
  const navigate = useNavigate();
  return <OrdersPage search={Route.useSearch()} onSearch={(search) => void navigate({ to: '/orders', search })} />;
}
