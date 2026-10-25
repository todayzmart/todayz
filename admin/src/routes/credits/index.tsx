import { createFileRoute } from '@tanstack/react-router';
import { CreditsPage } from '../../credits/CreditsPage';
import { validateCreditsSearch } from '../../credits/search';

export const Route = createFileRoute('/credits/')({ validateSearch: validateCreditsSearch, component: Credits });

function Credits() {
  return <CreditsPage search={Route.useSearch()} />;
}
