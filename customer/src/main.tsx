import '@todayz/tokens/styles.css';
import { htmlLang } from '@todayz/i18n';
import { configFromEnv, initTelemetry } from '@todayz/telemetry';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App';
import { initialLocale, loadMessages } from './app/i18n';
import { makeQueryClient, makeRouter } from './app/router';
import { captureInstallPrompt } from './pwa/install';

// The install prompt waits for the 2nd order (06-screens.md); hold the browser's own one back now.
captureInstallPrompt();

const locale = initialLocale();
document.documentElement.lang = htmlLang(locale);

const queryClient = makeQueryClient();
const router = makeRouter(queryClient);

// Off unless VITE_RUM_ENDPOINT is set for a staging or production build (05-observability §2.5).
// RUM labels use the matched route id (`/r/$token`), never the raw path.
initTelemetry({ ...configFromEnv('customer', import.meta.env), routeId: () => router.state.matches.at(-1)?.routeId });

const root = document.getElementById('root');
if (!root) throw new Error('#root is missing from index.html');

void loadMessages(locale).then((messages) => {
  createRoot(root).render(
    <StrictMode>
      <App messages={messages} router={router} queryClient={queryClient} />
    </StrictMode>,
  );
});

// Service worker only in production builds (vite-plugin-pwa devOptions are off). The update
// toast ("A new version is ready") is built in Wave 1; until then a new worker waits.
if (import.meta.env.PROD) {
  void import('virtual:pwa-register').then(({ registerSW }) => registerSW({ immediate: true }));
}
