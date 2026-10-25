import { ApiError } from '@todayz/api-client';
import { QueryClient } from '@tanstack/react-query';
import { createRouter, type RouterHistory } from '@tanstack/react-router';
import { routeTree } from '../routeTree.gen';
import { RouteError } from './CrashFallback';

export interface RouterContext {
  queryClient: QueryClient;
}

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      // A 4xx answer (missing, forbidden) will not change on a second try; a 5xx or network error might.
      queries: { staleTime: 30_000, retry: (n, e) => n < 1 && !(e instanceof ApiError && e.status < 500), refetchOnWindowFocus: false },
    },
  });
}

export function makeRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    defaultErrorComponent: RouteError,
    scrollRestoration: true,
    ...(history ? { history } : {}),
  });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
