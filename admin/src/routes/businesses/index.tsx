import { createFileRoute } from '@tanstack/react-router';
import { BusinessesPage } from '../../businesses/BusinessesPage';
import { validateBusinessesSearch } from '../../businesses/search';

export const Route = createFileRoute('/businesses/')({ validateSearch: validateBusinessesSearch, component: Businesses });

function Businesses() {
  return <BusinessesPage search={Route.useSearch()} />;
}
