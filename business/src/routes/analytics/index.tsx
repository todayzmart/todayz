import { createFileRoute } from '@tanstack/react-router';
import { AnalyticsPage } from '../../analytics/AnalyticsPage';
import { validateAnalyticsSearch } from '../../analytics/search';

/** B-12 Analytics: tabs Sales and Insights (D-99). A static route outranks `/$section`. */
export const Route = createFileRoute('/analytics/')({
  validateSearch: validateAnalyticsSearch,
  component: function AnalyticsRoute() {
    return <AnalyticsPage search={Route.useSearch()} />;
  },
});
