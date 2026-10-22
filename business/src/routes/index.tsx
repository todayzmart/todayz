import { createFileRoute, redirect } from '@tanstack/react-router';

/**
 * B-04 Orders is the business landing page until W3-03 builds B-03 Overview (D-84). The redirect
 * replaces the history entry, so Back never lands on `/` and bounces again.
 */
export const Route = createFileRoute('/')({
  beforeLoad: () => {
    // search: true keeps the query (e.g. ?locale=pseudo) across the redirect.
    throw redirect({ to: '/$section', params: { section: 'orders' }, search: true, replace: true });
  },
});
