import { createFileRoute } from '@tanstack/react-router';
import { InboxPage } from '../../support/InboxPage';
import { validateSupportSearch } from '../../support/search';

export const Route = createFileRoute('/support/')({ validateSearch: validateSupportSearch, component: Support });

function Support() {
  return <InboxPage search={Route.useSearch()} />;
}
