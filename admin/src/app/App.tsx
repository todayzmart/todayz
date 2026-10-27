import { ErrorBoundary, SessionProvider, UpdateToastHost, type Session, type UpdateController } from '@todayz/auth';
import { FORMAT_LOCALE, type Messages } from '@todayz/i18n';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { ToastProvider } from '@todayz/ui';
import { useEffect } from 'react';
import { IntlProvider } from 'react-intl';
import { CrashFallback } from './CrashFallback';
import { useUpdateLabels } from './labels';
import type { makeRouter } from './router';
import { UpdateContext } from './update-context';

/** A missing key is a bug: tests fail on it, dev and prod log it (the smoke test fails on console errors). */
const onIntlError = (err: Error): void => {
  if (import.meta.env.MODE === 'test') throw err;
  console.error(err);
};

interface AppProps {
  messages: Messages;
  router: ReturnType<typeof makeRouter>;
  queryClient: QueryClient;
  session: Session;
  update: UpdateController;
}

function UpdateToast({ update }: { update: UpdateController }) {
  return <UpdateToastHost controller={update} labels={useUpdateLabels()} />;
}

/**
 * Formatting (numbers, dates, ₹) always uses en-IN; the pseudo-locale only swaps the messages,
 * so no extra Intl locale data is needed for it.
 */
export function App({ messages, router, queryClient, session, update }: AppProps) {
  // Another tab signed in as someone else: nothing cached for the previous person may stay.
  useEffect(() => session.onUserChange(() => queryClient.clear()), [session, queryClient]);
  return (
    <IntlProvider locale={FORMAT_LOCALE} defaultLocale={FORMAT_LOCALE} messages={messages} onError={onIntlError}>
      <ErrorBoundary fallback={(reset) => <CrashFallback reset={reset} />}>
        <SessionProvider session={session}>
          <UpdateContext.Provider value={update}>
            <ToastProvider>
              <QueryClientProvider client={queryClient}>
                <RouterProvider router={router} />
                <UpdateToast update={update} />
              </QueryClientProvider>
            </ToastProvider>
          </UpdateContext.Provider>
        </SessionProvider>
      </ErrorBoundary>
    </IntlProvider>
  );
}
