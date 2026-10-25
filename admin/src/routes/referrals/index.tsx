import { createFileRoute } from '@tanstack/react-router';
import { ReferralsPage } from '../../referrals/ReferralsPage';
import { validateReferralsSearch } from '../../referrals/search';

export const Route = createFileRoute('/referrals/')({ validateSearch: validateReferralsSearch, component: Referrals });

function Referrals() {
  return <ReferralsPage search={Route.useSearch()} />;
}
