import { defineConfig, devices } from '@playwright/test';

// W4-04: visual baselines of the key screens in en and the pseudo-locale (en-XA), against the running
// stack (`make dev`) on the seed. Baselines live in ./baselines (committed, synthetic data only).
// Refresh the ones you changed on purpose with `pnpm --filter @todayz/e2e-visual visual -- --update-snapshots <grep>`.
process.env['TZ_JOURNEYS_RUN'] ??= String(Date.now());

export default defineConfig({
  testDir: '.',
  testMatch: /visual\.spec\.ts/,
  snapshotPathTemplate: '{testDir}/baselines/{arg}{ext}',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 15_000, toHaveScreenshot: { maxDiffPixelRatio: 0.02, animations: 'disabled', caret: 'hide' } },
  reporter: [['list']],
  globalTeardown: '../a11y/global-teardown.ts',
  use: { ...devices['Desktop Chrome'], ignoreHTTPSErrors: true, trace: 'off', video: 'off', screenshot: 'off' },
});
