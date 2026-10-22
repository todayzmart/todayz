import { FORMAT_LOCALE, type Messages } from '@todayz/i18n';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { IntlProvider } from 'react-intl';
import type { makeRouter } from './router';

/** A missing key is a bug: tests fail on it, dev and prod log it (the smoke test fails on console errors). */
const onIntlError = (err: Error): void => {
  if (import.meta.env.MODE === 'test') throw err;
  console.error(err);
};

interface AppProps {
  messages: Messages;
  router: ReturnType<typeof makeRouter>;
  queryClient: QueryClient;
}

/**
 * Formatting (numbers, dates, ₹) always uses en-IN; the pseudo-locale only swaps the messages,
 * so no extra Intl locale data is needed for it.
 */
export function App({ messages, router, queryClient }: AppProps) {
  return (
    <IntlProvider locale={FORMAT_LOCALE} defaultLocale={FORMAT_LOCALE} messages={messages} onError={onIntlError}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </IntlProvider>
  );
}
