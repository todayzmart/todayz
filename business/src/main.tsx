import '@todayz/tokens/styles.css';
import '@todayz/ui/styles.css';
import { createUpdateController } from '@todayz/auth';
import { applyLocale } from '@todayz/i18n';
import { configFromEnv, initTelemetry } from '@todayz/telemetry';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App';
import { initialLocale, loadMessages } from './app/i18n';
import { makeQueryClient, makeRouter } from './app/router';
import { makeSession } from './app/session';

const locale = initialLocale();
// <html lang> and the script's fonts; a stored choice from B-14 Settings wins over the browser's languages.
void applyLocale(locale);

const queryClient = makeQueryClient();
const router = makeRouter(queryClient);
// The access token lives in this object's memory only; the refresh cookie is httpOnly.
const session = makeSession();
const update = createUpdateController();

// Off unless VITE_RUM_ENDPOINT is set for a staging or production build (05-observability §2.5).
// RUM labels use the matched route id (`/orders/$orderId`), never the raw path.
initTelemetry({ ...configFromEnv('business', import.meta.env), routeId: () => router.state.matches.at(-1)?.routeId });

const root = document.getElementById('root');
if (!root) throw new Error('#root is missing from index.html');

void loadMessages(locale).then((messages) => {
  createRoot(root).render(
    <StrictMode>
      <App locale={locale} messages={messages} router={router} queryClient={queryClient} session={session} update={update} />
    </StrictMode>,
  );
});

// Service worker only in production builds (vite-plugin-pwa devOptions are off). A new worker
// waits; the "A new version is ready" toast reloads only when the person taps it, and not while a
// sale is open (G9, D-98).
if (import.meta.env.PROD) {
  void import('virtual:pwa-register').then(({ registerSW }) => {
    const updateSW = registerSW({ immediate: true, onNeedRefresh: () => update.notifyReady() });
    update.setUpdater(updateSW);
  });
}
