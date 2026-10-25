import { createFileRoute } from '@tanstack/react-router';
import { DemandPage } from '../../demand/DemandPage';
import { validateDemandSearch } from '../../demand/search';

export const Route = createFileRoute('/demand/')({ validateSearch: validateDemandSearch, component: Demand });

function Demand() {
  return <DemandPage search={Route.useSearch()} />;
}
