import { ErrorBoundary, SessionProvider, UpdateToastHost, type Session, type UpdateController } from '@todayz/auth';
import { FORMAT_LOCALE, type Locale, type MessageLocale, type Messages } from '@todayz/i18n';
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { Button, ToastProvider } from '@todayz/ui';
import { useCallback, useMemo, useState } from 'react';
import { IntlProvider, useIntl } from 'react-intl';
import styles from '../shell/shell.module.css';
import { loadMessages } from './i18n';
import { useUpdateLabels } from './labels';
import { LocaleContext, rememberLocale, type LocaleApi } from './locale';
import { patchMe } from './me';
import type { makeRouter } from './router';
import { setSessionLanguage } from './session';
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
    <div role="alert" className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'app.error.title' })}</h1>
      <p>{intl.formatMessage({ id: 'app.error.body' })}</p>
      <Button variant="primary" onClick={reset}>
        {intl.formatMessage({ id: 'app.error.retry' })}
      </Button>
    </div>
  );
}

/**
 * Formatting (numbers, dates, ₹) always uses en-IN; the pseudo-locale only swaps the messages,
 * so no extra Intl locale data is needed for it.
 */
export function App({ locale: initial = 'en', messages: initialMessages, router, queryClient, session, update }: AppProps) {
  const [current, setCurrent] = useState<{ locale: MessageLocale; messages: Messages }>({
    locale: initial,
    messages: initialMessages,
  });

  const choose = useCallback(
    async (next: Locale) => {
      rememberLocale(next);
      setSessionLanguage(next);
      const messages = await loadMessages(next);
      setCurrent({ locale: next, messages });
      // The account keeps the language too (AU-11). A failure here only means the device choice stands.
      if (session.getState().status === 'signedIn') void patchMe(session, { preferredLang: next }).catch(() => undefined);
    },
    [session],
  );
  const api = useMemo<LocaleApi>(() => ({ locale: current.locale, choose }), [current.locale, choose]);

  return (
    <IntlProvider locale={FORMAT_LOCALE} defaultLocale={FORMAT_LOCALE} messages={current.messages} onError={onIntlError}>
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
