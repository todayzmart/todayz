import { playwrightPreset } from '@todayz/config/playwright';
import { defineConfig } from '@playwright/test';

// Smoke: the e2e-mode build (build:e2e) under `vite preview --mode e2e` on a random free port (never 5301).
// The sign-in journeys (*.stack.spec.ts) need the running stack and run from playwright.stack.config.ts.
export default defineConfig({
  ...playwrightPreset({ command: (port) => `node ./node_modules/vite/bin/vite.js preview --mode e2e --port ${port} --strictPort` }),
  testIgnore: /.*\.stack\.spec\.ts/,
});
