// Lighthouse CI against the built dist/ (filesystem upload only, nothing leaves the machine).
// Uses the Playwright-installed Chromium so no system Chrome is needed; the Turnstile host is blocked
// in lighthouserc.json so the run never touches the network. Scores must be >= 95 in every category,
// on mobile (Lighthouse's default emulation, as configured in lighthouserc.json) and on desktop
// (the same URLs with the desktop preset). One documented exception (D-102, site/CLAUDE.md): mobile
// performance on /hi/, /kn/ and /te/ is gated at 85 until the Indic fonts are subset; desktop has none.
// The machine-draft locales are noindex in this (e2e) build by design, so on /hi/, /kn/, /te/ and
// /ta/ the SEO category is replaced by its other audits, each required to pass; production builds
// do not contain draft locales at all. Reports land in .lighthouseci/{mobile,desktop}/.
import { chromium } from '@playwright/test';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
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

/** Desktop: the mobile config with the desktop preset and no performance exception. */
function desktopConfig() {
  const config = JSON.parse(readFileSync(join(root, 'lighthouserc.json'), 'utf8'));
  config.ci.collect.settings = { ...config.ci.collect.settings, preset: 'desktop' };
  for (const entry of config.ci.assert.assertMatrix) entry.assertions['categories:performance'] = ['error', { minScore: 0.95 }];
  config.ci.upload = { ...config.ci.upload, outputDir: './.lighthouseci/desktop' };
  mkdirSync(join(root, '.lighthouseci'), { recursive: true });
  const file = join(root, '.lighthouseci', 'desktop.lighthouserc.json');
  writeFileSync(file, `${JSON.stringify(config, null, 2)}\n`);
  return file;
}

const runs = [
  { name: 'mobile', config: 'lighthouserc.json' },
  { name: 'desktop', config: desktopConfig() },
];
let failed = false;
for (const run of runs) {
  console.log(`\nLighthouse (${run.name})`);
  const result = spawnSync(join(root, 'node_modules', '.bin', 'lhci'), ['autorun', `--config=${run.config}`], {
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
