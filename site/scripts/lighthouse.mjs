// Lighthouse CI against the built dist/ (filesystem upload only, nothing leaves the machine).
// Uses the Playwright-installed Chromium so no system Chrome is needed; the Turnstile host is blocked
// in lighthouserc.json so the run never touches the network. Scores must be >= 95 in every category,
// on mobile (Lighthouse's default emulation, as configured in lighthouserc.json) and on desktop
// (the same config with the desktop preset). Reports land in .lighthouseci/{mobile,desktop}/.
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
const env = { ...process.env, CHROME_PATH: chromePath(), LHCI_NO_TELEMETRY: '1' };
const runs = [
  { name: 'mobile', args: [] },
  { name: 'desktop', args: ['--collect.settings.preset=desktop', '--upload.outputDir=./.lighthouseci/desktop'] },
];
let failed = false;
for (const run of runs) {
  console.log(`\nLighthouse (${run.name})`);
  const result = spawnSync(join(root, 'node_modules', '.bin', 'lhci'), ['autorun', '--config=lighthouserc.json', ...run.args], {
    cwd: root,
    stdio: 'inherit',
    env,
  });
  if (result.status !== 0) {
    console.error(`Lighthouse (${run.name}) failed`);
    failed = true;
  }
}
process.exit(failed ? 1 : 0);
