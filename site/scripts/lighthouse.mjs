// Lighthouse CI against the built dist/ (filesystem upload only, nothing leaves the machine).
// Uses the Playwright-installed Chromium so no system Chrome is needed; the Turnstile host is blocked
// in lighthouserc.json so the run never touches the network. Scores must be >= 95 in every category.
import { chromium } from '@playwright/test';
import { spawnSync } from 'node:child_process';
import { existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

/** The full Chromium if installed, else the newest headless shell Playwright downloaded. */
function chromePath() {
  const full = chromium.executablePath();
  if (existsSync(full)) return full;
  const cache = /^(.*ms-playwright)[\\/]/.exec(full)?.[1];
  if (cache) {
    const shells = readdirSync(cache).filter((d) => d.startsWith('chromium_headless_shell-')).sort().reverse();
    for (const dir of shells) {
      const folder = readdirSync(join(cache, dir)).find((d) => d.startsWith('chrome-headless-shell-'));
      const bin = folder ? join(cache, dir, folder, 'chrome-headless-shell') : '';
      if (bin && existsSync(bin)) return bin;
    }
  }
  throw new Error('No Chromium found: run `pnpm exec playwright install chromium`');
}

const root = join(import.meta.dirname, '..');
const result = spawnSync(join(root, 'node_modules', '.bin', 'lhci'), ['autorun', '--config=lighthouserc.json'], {
  cwd: root,
  stdio: 'inherit',
  env: { ...process.env, CHROME_PATH: chromePath(), LHCI_NO_TELEMETRY: '1' },
});
process.exit(result.status ?? 1);
