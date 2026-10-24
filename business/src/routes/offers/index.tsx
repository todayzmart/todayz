import { createFileRoute } from '@tanstack/react-router';
import { validateOffersSearch } from '../../offers/deeplink';
import { OffersPage } from '../../offers/OffersPage';

/** B-08 Offers and banners; the search parameters are the D-99 insight deep links (prefill only). */
export const Route = createFileRoute('/offers/')({
  validateSearch: validateOffersSearch,
  component: function OffersRoute() {
    return <OffersPage search={Route.useSearch()} />;
  },
});
