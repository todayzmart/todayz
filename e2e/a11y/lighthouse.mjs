// W4-04: Lighthouse on the three app shells (the site has its own: site/scripts/lighthouse.mjs).
// Serves each app's e2e-mode build (`pnpm --filter <app> build:e2e` first) with `vite preview` on a
// free local port, opens it in Playwright's Chromium with the first-run choices in storage (English,
// the seed's QA point), and points the Lighthouse CLI at that browser. Mobile (Lighthouse default
// emulation) and desktop preset. Reports go to e2e/a11y/out/lighthouse/ (git-ignored); nothing is
// uploaded. The apps' API calls go to the stack; a preview origin the API does not allow gets no
// data, so these scores are for the app shell and first paint, not for filled lists.
// Usage: node lighthouse.mjs [customer|business|admin ...]   (LIGHTHOUSE_BIN overrides the CLI path)
import { chromium } from '@playwright/test';
import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { join } from 'node:path';

const root = join(import.meta.dirname, '..', '..');
const out = join(import.meta.dirname, 'out', 'lighthouse');
mkdirSync(out, { recursive: true });
const bin = process.env.LIGHTHOUSE_BIN ?? join(root, 'node_modules', '.pnpm', 'lighthouse@12.6.1', 'node_modules', 'lighthouse', 'cli', 'index.js');
if (!existsSync(bin)) throw new Error(`Lighthouse CLI not found at ${bin}: set LIGHTHOUSE_BIN`);

const PLACE = JSON.stringify({ kind: 'gps', lat: 12.9719, lng: 77.6412 });
const APPS = {
  customer: { dir: 'customer', routes: ['/', '/search?q=atta', '/sign-in'], storage: { 'todayz.lang': 'en', 'todayz.place': PLACE } },
  business: { dir: 'business', routes: ['/sign-in'], storage: { 'todayz.lang': 'en' } },
  admin: { dir: 'admin', routes: ['/sign-in'], storage: { 'todayz.lang': 'en' } },
};

/** The full Chromium if installed, else the newest headless shell Playwright downloaded. */
function chromePath() {
  const full = chromium.executablePath();
  if (existsSync(full)) return full;
  const cache = /^(.*ms-playwright)[\\/]/.exec(full)?.[1];
  for (const dir of cache ? readdirSync(cache).filter((d) => d.startsWith('chromium_headless_shell-')).sort().reverse() : []) {
    const folder = readdirSync(join(cache, dir)).find((d) => d.startsWith('chrome-headless-shell-'));
    const bin = folder ? join(cache, dir, folder, 'chrome-headless-shell') : '';
    if (bin && existsSync(bin)) return bin;
  }
  throw new Error('No Chromium found: run `pnpm exec playwright install chromium`');
}

function freePort() {
  return new Promise((res) => {
    const s = createServer();
    s.listen(0, '127.0.0.1', () => {
      const { port } = s.address();
      s.close(() => res(port));
    });
  });
}

const wanted = process.argv.slice(2).filter((a) => a in APPS);
const summary = [];
for (const name of wanted.length ? wanted : Object.keys(APPS)) {
  const app = APPS[name];
  const dir = join(root, app.dir);
  const port = await freePort();
  const server = spawn('node', ['./node_modules/vite/bin/vite.js', 'preview', '--mode', 'e2e', '--port', String(port), '--strictPort', '--host', '127.0.0.1'], { cwd: dir, stdio: 'ignore' });
  await new Promise((r) => setTimeout(r, 2500));
  const origin = `http://127.0.0.1:${port}`;
  for (const preset of ['mobile', 'desktop']) {
    // Seed a profile with the first-run choices, then let Lighthouse launch Chrome on that profile.
    const profile = mkdtempSync(join(tmpdir(), 'tz-lh-'));
    const seeded = await chromium.launchPersistentContext(profile, { headless: true, args: ['--no-sandbox'] });
    const page = await seeded.newPage();
    await page.goto(`${origin}/sign-in`);
    await page.evaluate((s) => Object.entries(s).forEach(([k, v]) => localStorage.setItem(k, v)), app.storage);
    await seeded.close();
    for (const route of app.routes) {
      const slug = `${name}${route === '/' ? '-home' : route.replace(/[^a-z0-9]+/gi, '-')}-${preset}`;
      const file = join(out, `${slug}.json`);
      const args = [`${origin}${route}`, `--chrome-flags=--headless --no-sandbox --user-data-dir=${profile}`, '--disable-storage-reset', '--output=json', `--output-path=${file}`, '--quiet', '--only-categories=performance,accessibility,best-practices'];
      if (preset === 'desktop') args.push('--preset=desktop');
      const r = spawnSync('node', [bin, ...args], { cwd: out, encoding: 'utf8', timeout: 150000, env: { ...process.env, CHROME_PATH: chromePath(), LHCI_NO_TELEMETRY: '1' } });
      if (r.status !== 0) {
        summary.push({ app: name, route, preset, error: (r.stderr || String(r.error ?? r.signal)).slice(-300) });
        continue;
      }
      const j = JSON.parse(readFileSync(file, 'utf8'));
      const a = j.audits;
      summary.push({
        app: name,
        route,
        preset,
        performance: Math.round(j.categories.performance.score * 100),
        accessibility: Math.round(j.categories.accessibility.score * 100),
        bestPractices: Math.round(j.categories['best-practices'].score * 100),
        lcpMs: Math.round(a['largest-contentful-paint'].numericValue),
        cls: Number(a['cumulative-layout-shift'].numericValue.toFixed(3)),
        tbtMs: Math.round(a['total-blocking-time'].numericValue),
      });
    }
    rmSync(profile, { recursive: true, force: true });
  }
  server.kill();
}
writeFileSync(join(out, 'summary.json'), `${JSON.stringify(summary, null, 2)}\n`);
console.table(summary);
