import { createFileRoute } from '@tanstack/react-router';
import { validateStockSearch } from '../../stock/deeplink';
import { StockPage } from '../../stock/StockPage';

/** B-07 Stock, with the D-99 insight deep links as search parameters. A static route outranks `/$section`. */
export const Route = createFileRoute('/stock/')({
  validateSearch: validateStockSearch,
  component: RouteComponent,
});

function RouteComponent() {
  return <StockPage search={Route.useSearch()} />;
}
