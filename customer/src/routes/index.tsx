import { browserStore, hasChosenLocale } from '@todayz/i18n';
import { createFileRoute, redirect } from '@tanstack/react-router';
import { Home } from '../home/Home';

export const Route = createFileRoute('/')({
  // `?category=` filters the shops on Home (C-03); anything else is ignored.
  validateSearch: (s: Record<string, unknown>): { category?: string } =>
    typeof s['category'] === 'string' && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(s['category']) ? { category: s['category'] } : {},
  // First run: ask for the language once (C-01). `?locale=` in the URL is an explicit choice for that visit.
  beforeLoad: () => {
    if (!hasChosenLocale(browserStore()) && !new URLSearchParams(window.location.search).has('locale')) {
      throw redirect({ to: '/language' });
    }
  },
  component: HomeRoute,
});

function HomeRoute() {
  const { category } = Route.useSearch();
  return <Home category={category} />;
}
