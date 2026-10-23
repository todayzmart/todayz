import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';

/** The self-hosted Figtree file, as Vite names it (figtree-latin-wght-normal-<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

/**
 * The console is for signed-in staff who passed the authenticator step. The smoke run has no API, so
 * the session refresh is answered here: with a grant (signed in) or a 401 (signed out). The API is on
 * another origin, so the cookie-carrying call needs the preflight and an explicit origin in the answer.
 */
const cors = (origin: string) => ({
  'access-control-allow-origin': origin,
  'access-control-allow-credentials': 'true',
  'access-control-allow-headers': 'authorization, content-type, x-tz-client, accept-language, accept',
  'access-control-allow-methods': 'POST, OPTIONS',
});

async function answer(page: Page, path: string, status: number, body: unknown) {
  await page.route(`**${path}`, (route) => {
    const headers = cors(route.request().headers()['origin'] ?? '*');
    if (route.request().method() === 'OPTIONS') return route.fulfill({ status: 204, headers });
    return route.fulfill({
      status,
      contentType: status === 200 ? 'application/json' : 'application/problem+json',
      headers,
      body: JSON.stringify(body),
    });
  });
}

const problem = (status: number, code: string) => ({ type: `about:blank#${code}`, title: code, status, code });

async function answerRefresh(page: Page, signedIn: boolean) {
  await (signedIn
    ? answer(page, '/v1/auth/refresh', 200, {
        accessToken: 'smoke-token',
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        tokenType: 'Bearer',
        userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
      })
    : answer(page, '/v1/auth/refresh', 401, problem(401, 'unauthorized')));
}

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await answerRefresh(page, true);
  // Routing hint only (the API checks the token): this tab already passed the authenticator step.
  await page.addInitScript(() => sessionStorage.setItem('todayz.admin.2fa', String(Date.now())));
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('desktop: sidebar with the Admin tag', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Pulse' })).toBeVisible();
  const side = page.getByRole('navigation', { name: 'Main' });
  await expect(side.getByText('Admin', { exact: true })).toBeVisible();
  await expect(side.getByRole('link', { name: 'Payouts' })).toBeVisible();
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'admin-desktop.png');
});

test('phone: sidebar collapses to 44px icon links with names and tooltips', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  const side = page.getByRole('navigation', { name: 'Main' });
  await expect(side.getByText('Admin', { exact: true })).toBeHidden();
  const businesses = side.getByRole('link', { name: 'Businesses' });
  await expect(businesses).toHaveAttribute('title', 'Businesses');
  const box = await businesses.boundingBox();
  expect(box?.width).toBeGreaterThanOrEqual(44);
  expect(box?.height).toBeGreaterThanOrEqual(44);
  // The rail shrink-wraps its 44px links: no horizontal scrollbar.
  expect(await side.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  await businesses.click();
  await expect(page).toHaveURL(/\/businesses$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Businesses' })).toBeVisible();
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'admin-phone.png');
});

test('pseudo-locale via ?locale=pseudo', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/?locale=pseudo');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/^\[.+\]$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'admin-pseudo-desktop.png');
});

test('is not indexable', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', /noindex/);
});

test('signed out: any page opens the sign-in screen (A-01)', async ({ page }) => {
  await answerRefresh(page, false);
  await page.goto('/businesses');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fbusinesses$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Admin' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
});

test('signed in without the authenticator step: held at the two-step check', async ({ page }) => {
  await page.addInitScript(() => sessionStorage.removeItem('todayz.admin.2fa'));
  await answer(page, '/v1/auth/totp/enrol', 409, problem(409, 'conflict'));
  await page.goto('/stores');
  await expect(page).toHaveURL(/\/two-step\?next=%2Fstores$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Two-step check' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
});
