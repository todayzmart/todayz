import { createRootRouteWithContext } from '@tanstack/react-router';
import type { RouterContext } from '../app/router';
import { Gate } from '../shell/Gate';
import { NotFound } from '../shell/NotFound';

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Gate,
  notFoundComponent: NotFound,
});
