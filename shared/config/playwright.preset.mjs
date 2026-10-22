// @todayz/config/playwright: smoke-test preset for the frontends (W0-05).
//
// Each app serves its production build with `vite preview` / `astro preview` on a random free
// port (other agents and the dev stack own the fixed ports), on 127.0.0.1 only, with Chromium.
import { devices, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { dirname, isAbsolute, join } from 'node:path';
import { fileURLToPath } from 'node:url';

/** A free TCP port on 127.0.0.1, picked by binding port 0 in a child process. */
export function freePort() {
  const out = execFileSync(
    process.execPath,
    ['-e', "const s=require('node:net').createServer();s.listen(0,'127.0.0.1',()=>{process.stdout.write(String(s.address().port));s.close()})"],
    { encoding: 'utf8' },
  );
  return Number(out);
}

/**
 * @param {{ command: (port: number) => string, testDir?: string, outputDir?: string }} opts
 * `command` starts the preview server on the given port, bound to 127.0.0.1.
 */
export function playwrightPreset({ command, testDir = './e2e', outputDir = './test-results' }) {
  // Workers re-evaluate the config; the env var keeps one port for the whole run.
  process.env.TZ_SMOKE_PORT ??= String(freePort());
  const port = Number(process.env.TZ_SMOKE_PORT);
  const baseURL = `http://127.0.0.1:${port}`;
  return {
    testDir,
    outputDir,
    fullyParallel: true,
    forbidOnly: true,
    retries: 0,
    workers: 2,
    reporter: [['list']],
    timeout: 30_000,
    use: {
      baseURL,
      locale: 'en-IN',
      timezoneId: 'Asia/Kolkata',
      colorScheme: 'light',
      // Block service workers so route interception sees every request the page makes.
      serviceWorkers: 'block',
      trace: 'off',
      screenshot: 'off',
      video: 'off',
    },
    projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
    webServer: {
      command: command(port),
      url: `${baseURL}/`,
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: 'ignore',
      stderr: 'pipe',
    },
  };
}

const LOCAL_HOSTS = new Set(['127.0.0.1', 'localhost', '[::1]']);

/** True for URLs the smoke tests may load: loopback hosts, *.localhost, data: and blob:. */
export function isLocalUrl(raw) {
  const url = new URL(raw);
  if (url.protocol === 'data:' || url.protocol === 'blob:') return true;
  return LOCAL_HOSTS.has(url.hostname) || url.hostname === 'localhost' || url.hostname.endsWith('.localhost');
}

/**
 * Watches a page for the smoke rules: every non-local request is aborted and recorded, and every
 * console error and uncaught page error is recorded. Every response is recorded too, so a test can
 * prove an asset really came from the app's own origin. Call before the first navigation.
 * @param {import('@playwright/test').Page} page
 */
export async function guardPage(page) {
  const external = [];
  const errors = [];
  const responses = [];
  await page.route('**/*', (route) => {
    const url = route.request().url();
    if (isLocalUrl(url)) return route.continue();
    external.push(url);
    return route.abort('blockedbyclient');
  });
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(`console: ${msg.text()}`);
  });
  page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  page.on('response', (res) => responses.push({ url: res.url(), status: res.status() }));
  return { external, errors, responses };
}

/**
 * Proves a self-hosted font really loaded: a 200 for a matching font file from the page's own
 * origin, and a FontFace of that family in document.fonts with status "loaded".
 * (document.fonts.check() is not enough: it returns true when no @font-face matches at all.)
 * @param {import('@playwright/test').Page} page
 * @param {{ responses: { url: string, status: number }[] }} guard  from guardPage()
 * @param {string} family  e.g. 'Figtree'
 * @param {RegExp} file    matched against the response path, e.g. /\/figtree-[^/]*\.woff2$/
 */
export async function expectFontLoaded(page, guard, family, file) {
  const origin = new URL(page.url()).origin;
  await expect
    .poll(
      () =>
        guard.responses.some((r) => {
          const url = new URL(r.url);
          return r.status === 200 && url.origin === origin && file.test(url.pathname);
        }),
      { message: `${family}: a 200 for ${file} from ${origin}` },
    )
    .toBe(true);
  await expect
    .poll(
      () =>
        page.evaluate(
          (fam) => [...document.fonts].some((f) => f.family.replace(/"/g, '') === fam && f.status === 'loaded'),
          family,
        ),
      { message: `${family}: a loaded FontFace in document.fonts` },
    )
    .toBe(true);
}

const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

/**
 * Where smoke screenshots go. By default the git-ignored test-results/screenshots of the package
 * under test, so `make e2e` never rewrites committed files. Set TZ_QA_DIR (absolute, or relative
 * to the repo root, e.g. TZ_QA_DIR=docs/qa/W0-05) to refresh the QA evidence on purpose.
 */
export function qaDir() {
  const dir = process.env.TZ_QA_DIR;
  if (dir) return isAbsolute(dir) ? dir : join(REPO_ROOT, dir);
  return join(process.cwd(), 'test-results', 'screenshots');
}

/**
 * A screenshot into qaDir() with CSS animations and transitions finished, so the image is stable.
 * @param {import('@playwright/test').Page} page
 * @param {string} name  file name, e.g. 'customer-phone.png'
 * @param {{ fullPage?: boolean }} [opts]
 */
export function shot(page, name, opts = {}) {
  return page.screenshot({ path: join(qaDir(), name), animations: 'disabled', ...opts });
}

export const VIEWPORTS = {
  phone: { width: 375, height: 812 },
  desktop: { width: 1280, height: 800 },
};

export default playwrightPreset;
