import { defineConfig, devices } from '@playwright/test';
import { playwrightPreset } from '@todayz/config/playwright';

// Visual and accessibility checks against the built Storybook (run `pnpm build-storybook` first).
// The server runs from this folder (Playwright's default cwd is the config's directory).
const preset = playwrightPreset({
  command: (port) => `node serve.mjs ${port}`,
  testDir: '.',
  outputDir: './out',
});

export default defineConfig({
  ...preset,
  // Other worktrees build in parallel: keep the worker count modest.
  workers: 4,
  timeout: 60_000,
  // Desktop Chrome at device scale 1, so design and story pixels line up one to one.
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
