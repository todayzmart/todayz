import { createFileRoute, notFound } from '@tanstack/react-router';
import { KindPage } from '../../jobs/KindPage';
import { KIND, validateKindSearch } from '../../jobs/search';

export const Route = createFileRoute('/jobs/$kind')({
  validateSearch: validateKindSearch,
  beforeLoad: ({ params }) => {
    if (!KIND.test(params.kind)) throw notFound();
  },
  component: Kind,
});

function Kind() {
  const { kind } = Route.useParams();
  return <KindPage key={kind} kind={kind} search={Route.useSearch()} />;
}
