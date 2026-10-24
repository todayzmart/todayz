import { defineConfig, devices } from '@playwright/test';

// W2-13: the Wave 2 E2E gate. Multi-context journeys against the running local stack (`make dev`):
// the three apps, the API, and the mocks server for SMS codes. One worker, in file order: the
// journeys share one seed and one test clock, and the ones that move the clock run last (20-*).
// Caddy's local CA is not in Playwright's browser, hence ignoreHTTPSErrors.
//
// Nothing secret goes into the output: traces and videos are off (they would hold codes and
// tokens), and a failure screenshot shows only synthetic seed data.

// One id per gate run, shared by the runner and its workers: the session file lives under it.
process.env['TZ_JOURNEYS_RUN'] ??= String(Date.now());

export default defineConfig({
  testDir: './specs',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['json', { outputFile: 'test-results/report.json' }]],
  globalSetup: './global-setup.ts',
  globalTeardown: './global-teardown.ts',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: process.env['TZ_CUSTOMER_URL'] ?? 'https://app.todayz.localhost',
    ignoreHTTPSErrors: true,
    locale: 'en-IN',
    timezoneId: 'Asia/Kolkata',
    viewport: { width: 390, height: 844 },
    // A control that is not on the screen fails the step in seconds, not at the 3-minute test timeout.
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    trace: 'off',
    video: 'off',
    screenshot: 'only-on-failure',
  },
});
