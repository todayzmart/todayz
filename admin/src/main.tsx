import '@todayz/tokens/styles.css';
import '@todayz/ui/styles.css';
import { createUpdateController } from '@todayz/auth';
import { htmlLang } from '@todayz/i18n';
import { configFromEnv, initTelemetry } from '@todayz/telemetry';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App';
import { initialLocale, loadMessages } from './app/i18n';
import { makeQueryClient, makeRouter } from './app/router';
import { createSecondFactor } from './app/second-factor';
import { makeSession } from './app/session';

const locale = initialLocale();
document.documentElement.lang = htmlLang(locale);

const queryClient = makeQueryClient();
const router = makeRouter(queryClient);
// The access token lives in this object's memory only; the refresh cookie is httpOnly.
const session = makeSession();
const update = createUpdateController();
const factor = createSecondFactor();

// Off unless VITE_RUM_ENDPOINT is set for a staging or production build (05-observability §2.5).
// RUM labels use the matched route id (`/businesses/$businessId`), never the raw path.
initTelemetry({ ...configFromEnv('admin', import.meta.env), routeId: () => router.state.matches.at(-1)?.routeId });

const root = document.getElementById('root');
if (!root) throw new Error('#root is missing from index.html');

void loadMessages(locale).then((messages) => {
  createRoot(root).render(
    <StrictMode>
      <App messages={messages} router={router} queryClient={queryClient} session={session} update={update} factor={factor} />
    </StrictMode>,
  );
});

// Service worker only in production builds (vite-plugin-pwa devOptions are off). A new worker
// waits; the "A new version is ready" toast reloads only when the person taps it, and not while a
// form has unsaved work.
if (import.meta.env.PROD) {
  void import('virtual:pwa-register').then(({ registerSW }) => {
    const updateSW = registerSW({ immediate: true, onNeedRefresh: () => update.notifyReady() });
    update.setUpdater(updateSW);
  });
}
