import { defineConfig, devices } from '@playwright/test';

// W4-04: axe on every route at three viewports against the running stack (`make dev`). One worker:
// the people sign in once per run through the mocks' OTP (20 sends per IP per hour). Output holds
// rule ids, selectors and HTML snippets of the synthetic seed only.
process.env['TZ_JOURNEYS_RUN'] ??= String(Date.now());

export default defineConfig({
  testDir: '.',
  testMatch: /a11y\.spec\.ts/,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: [['list']],
  globalTeardown: './global-teardown.ts',
  use: { ...devices['Desktop Chrome'], ignoreHTTPSErrors: true, trace: 'off', video: 'off', screenshot: 'off' },
});
