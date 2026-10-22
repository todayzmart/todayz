import { createRootRouteWithContext } from '@tanstack/react-router';
import type { RouterContext } from '../app/router';
import { NotFound } from '../shell/NotFound';
import { Shell } from '../shell/Shell';

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Shell,
  notFoundComponent: NotFound,
});
