import { createFileRoute } from '@tanstack/react-router';
import { HelpPage } from '../../help/HelpPage';

interface HelpSearch {
  tab?: 'tickets';
}

function HelpRoute() {
  const search = Route.useSearch();
  return <HelpPage tab={search.tab ?? 'guide'} />;
}

/** B-15 Help: guide, FAQs and tickets. A static route outranks `/$section`. */
export const Route = createFileRoute('/help/')({
  validateSearch: (s: Record<string, unknown>): HelpSearch => (s.tab === 'tickets' ? { tab: 'tickets' } : {}),
  component: HelpRoute,
});
