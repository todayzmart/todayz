// The built pages under the real Content-Security-Policy. `astro preview` does not apply Cloudflare's
// dist/_headers, so this test adds the CSP from that file to every page response itself and fails on
// any `securitypolicyviolation`. Turnstile stays stubbed (helpers.ts), but its script is served from
// its real origin, so the policy must allow it; the lead form posts to the stubbed local API, so
// connect-src must allow that too.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { guardPage, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { LEADS_URL, TEST_PHONE, blockTurnstile } from './helpers.ts';

const DIST = join(import.meta.dirname, '..', 'dist');

/** The Content-Security-Policy of the `/*` rule in dist/_headers. */
function builtPolicy(): string {
  const file = readFileSync(join(DIST, '_headers'), 'utf8');
  const rule = file.split(/\n(?=\/)/).find((r) => r.startsWith('/*\n'));
  const csp = rule?.match(/^ {2}Content-Security-Policy: (.+)$/m)?.[1];
  if (!csp) throw new Error('dist/_headers has no Content-Security-Policy for /*');
  return csp;
}

const PAGES = ['/', '/for-retailers/', '/how-it-works/', '/pricing/', '/about/', '/contact/', '/legal/privacy/', '/hi/', '/te/', '/no-such-page/'];

let guard: PageGuard;

test.beforeEach(async ({ page, baseURL }) => {
  guard = await guardPage(page);
  await blockTurnstile(page);
  const csp = builtPolicy();
  const origin = new URL(baseURL ?? 'http://127.0.0.1').origin;
  await page.route(
    (url) => url.origin === origin,
    async (route) => {
      if (route.request().resourceType() !== 'document') return route.fallback();
      const response = await route.fetch();
      return route.fulfill({ response, headers: { ...response.headers(), 'content-security-policy': csp } });
    },
  );
  await page.addInitScript(() => {
    const seen: string[] = [];
    (window as unknown as { cspViolations: string[] }).cspViolations = seen;
    document.addEventListener('securitypolicyviolation', (e) => {
      seen.push(`${e.effectiveDirective} blocked ${e.blockedURI || '(inline)'} at ${e.sourceFile}:${e.lineNumber}`);
    });
  });
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

const violations = (page: Page) =>
  page.evaluate(() => (window as unknown as { cspViolations: string[] }).cspViolations);

test('the CSP from dist/_headers is applied to the pages under test', async ({ page }) => {
  const response = await page.goto('/');
  expect(response?.headers()['content-security-policy']).toBe(builtPolicy());
  expect(builtPolicy()).toContain('https://challenges.cloudflare.com');
});

for (const path of PAGES) {
  test(`${path} runs under the built CSP without a violation`, async ({ page }) => {
    await page.goto(path);
    await page.waitForLoadState('load');
    expect(await violations(page)).toEqual([]);
    // The 404 page is served with status 404, which the browser logs.
    const errors = guard.errors.filter((e) => !(path === '/no-such-page/' && /404/.test(String(e))));
    expect(errors, 'console and page errors').toEqual([]);
  });
}

test('the lead form loads Turnstile and posts to the API under the built CSP', async ({ page }) => {
  let posted = 0;
  await page.route(LEADS_URL, (route) => {
    posted += 1;
    return route.fulfill({ status: 201, body: '{}' });
  });
  await page.goto('/contact/');
  const form = page.locator('form[data-lead-form]').first();
  await expect(form.getByRole('button', { name: 'Send details' })).toBeEnabled();
  await expect(form.locator('.cf-turnstile input[name="cf-turnstile-response"]'), 'the Turnstile script ran').toHaveCount(1);
  await form.getByLabel('Your name').fill('Asha Rao');
  await form.getByLabel('Shop name').fill('Rao General Store');
  await form.getByLabel('Mobile number').fill(TEST_PHONE);
  await form.getByLabel('City').fill('Bengaluru');
  await form.getByRole('button', { name: 'Send details' }).click();
  await expect(form.getByRole('status')).toHaveText('Thank you. We have your details.');
  expect(posted).toBe(1);
  expect(await violations(page)).toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});
