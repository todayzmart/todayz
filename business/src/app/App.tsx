import { ErrorBoundary, SessionProvider, UpdateToastHost, type Session, type UpdateController } from '@todayz/auth';
import { FORMAT_LOCALE, intlTag, type Locale, type MessageLocale, type Messages } from '@todayz/i18n';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { Button, ToastProvider } from '@todayz/ui';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { IntlProvider, useIntl } from 'react-intl';
import { loadMessages } from './i18n';
import { useUpdateLabels } from './labels';
import { LocaleContext, rememberLocale, type LocaleApi } from './locale';
import type { makeRouter } from './router';
import { UpdateContext } from './update-context';

/** A missing key is a bug: tests fail on it, dev and prod log it (the smoke test fails on console errors). */
const onIntlError = (err: Error): void => {
  if (import.meta.env.MODE === 'test') throw err;
  console.error(err);
};

interface AppProps {
  locale?: MessageLocale;
  messages: Messages;
  router: ReturnType<typeof makeRouter>;
  queryClient: QueryClient;
  session: Session;
  update: UpdateController;
}

function UpdateToast({ update }: { update: UpdateController }) {
  return <UpdateToastHost controller={update} labels={useUpdateLabels()} />;
}

function CrashFallback({ reset }: { reset: () => void }) {
  const intl = useIntl();
  return (
    <div role="alert" style={{ padding: 'var(--space-4)' }}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'app.error.title' })}</h1>
      <p>{intl.formatMessage({ id: 'app.error.body' })}</p>
      <Button variant="primary" onClick={reset}>
        {intl.formatMessage({ id: 'app.error.retry' })}
      </Button>
    </div>
  );
}

/**
 * English and the pseudo-locale format with en-IN; the other languages use `<lang>-IN-u-nu-latn`, so
 * plurals and the words in times follow the language while digits stay Latin. ₹ amounts come
 * from `formatMoney`, which uses English (India) rules in every language.
 */
export function App({ locale: initial = 'en', messages: initialMessages, router, queryClient, session, update }: AppProps) {
  // Another tab signed in as someone else: nothing cached for the previous person may stay.
  useEffect(
    () =>
      session.onUserChange(() => {
        queryClient.clear();
      }),
    [session, queryClient],
  );
  const [current, setCurrent] = useState<{ locale: MessageLocale; messages: Messages }>({
    locale: initial,
    messages: initialMessages,
  });
  const choose = useCallback(async (next: Locale) => {
    rememberLocale(next);
    const messages = await loadMessages(next);
    setCurrent({ locale: next, messages });
  }, []);
  const api = useMemo<LocaleApi>(() => ({ locale: current.locale, choose }), [current.locale, choose]);

  const tag = current.locale === 'en' || current.locale === 'en-XA' ? FORMAT_LOCALE : intlTag(current.locale);

  return (
    <IntlProvider locale={tag} defaultLocale={FORMAT_LOCALE} messages={current.messages} timeZone="Asia/Kolkata" onError={onIntlError}>
      <ErrorBoundary fallback={(reset) => <CrashFallback reset={reset} />}>
        <SessionProvider session={session}>
          <UpdateContext.Provider value={update}>
            <LocaleContext.Provider value={api}>
              <ToastProvider>
                <QueryClientProvider client={queryClient}>
                  <RouterProvider router={router} />
                  <UpdateToast update={update} />
                </QueryClientProvider>
              </ToastProvider>
            </LocaleContext.Provider>
          </UpdateContext.Provider>
        </SessionProvider>
      </ErrorBoundary>
    </IntlProvider>
  );
}
