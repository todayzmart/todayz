import { createFileRoute } from '@tanstack/react-router';
import { RiskPage } from '../../risk/RiskPage';
import { validateRiskSearch } from '../../risk/search';

export const Route = createFileRoute('/risk/')({ validateSearch: validateRiskSearch, component: Risk });

function Risk() {
  return <RiskPage search={Route.useSearch()} />;
}
