import { createFileRoute } from '@tanstack/react-router';
import { StoresPage } from '../../stores/StoresPage';
import { validateStoresSearch } from '../../stores/search';

export const Route = createFileRoute('/stores/')({ validateSearch: validateStoresSearch, component: Stores });

function Stores() {
  return <StoresPage search={Route.useSearch()} />;
}
