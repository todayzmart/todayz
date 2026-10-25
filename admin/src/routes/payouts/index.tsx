import { createFileRoute } from '@tanstack/react-router';
import { PayoutsPage } from '../../payouts/PayoutsPage';
import { validatePayoutsSearch } from '../../payouts/search';

export const Route = createFileRoute('/payouts/')({ validateSearch: validatePayoutsSearch, component: Payouts });

function Payouts() {
  return <PayoutsPage search={Route.useSearch()} />;
}
