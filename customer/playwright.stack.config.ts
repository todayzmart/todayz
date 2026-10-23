import { defineConfig, devices } from '@playwright/test';

// Journeys against the running local stack (`make dev`): the app at https://app.todayz.localhost,
// the API at https://api.todayz.localhost and the mocks at http://127.0.0.1:4010 for the SMS code.
// Same-site hosts, so the httpOnly refresh cookie works as it does in production.
// Caddy's local CA is not in Playwright's browser, hence ignoreHTTPSErrors.
export default defineConfig({
  testDir: './e2e',
  testMatch: /.*\.stack\.spec\.ts/,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    ...devices['Desktop Chrome'],
    baseURL: process.env.TZ_APP_URL ?? 'https://app.todayz.localhost',
    ignoreHTTPSErrors: true,
    locale: 'en-IN',
    viewport: { width: 390, height: 844 },
  },
});
