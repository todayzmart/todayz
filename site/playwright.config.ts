import { playwrightPreset } from '@todayz/config/playwright';
import { defineConfig } from '@playwright/test';

// Smoke: the static build under `astro preview` on a random free port (never 5304).
export default defineConfig(
  playwrightPreset({
    command: (port) =>
      `ASTRO_TELEMETRY_DISABLED=1 ASTRO_DISABLE_UPDATE_CHECK=true node ./node_modules/astro/bin/astro.mjs preview --ignore-lock --host 127.0.0.1 --port ${port}`,
  }),
);
