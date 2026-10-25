import { createFileRoute } from '@tanstack/react-router';
import { JobsPage } from '../../jobs/JobsPage';
import { validateJobsSearch } from '../../jobs/search';

export const Route = createFileRoute('/jobs/')({ validateSearch: validateJobsSearch, component: Jobs });

function Jobs() {
  return <JobsPage search={Route.useSearch()} />;
}
