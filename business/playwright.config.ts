import { playwrightPreset } from '@todayz/config/playwright';
import { defineConfig } from '@playwright/test';

// Smoke: the production build under `vite preview` on a random free port (never 5301).
export default defineConfig(
  playwrightPreset({ command: (port) => `node ./node_modules/vite/bin/vite.js preview --port ${port} --strictPort` }),
);
