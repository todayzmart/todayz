import { createFileRoute, redirect } from '@tanstack/react-router';

/**
 * Overview lives at `/`. The sidebar and tab entry for it is still the `overview` section, so an
 * old `/overview` link replaces itself with `/` (query kept, e.g. ?locale=pseudo).
 */
export const Route = createFileRoute('/overview')({
  beforeLoad: () => {
    throw redirect({ to: '/', search: true, replace: true });
  },
});
