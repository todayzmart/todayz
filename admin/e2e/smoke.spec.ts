import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { answer, answerEmptyLists, answerMe, answerRefresh, problem } from './fake-api';

/** The self-hosted Figtree file, as Vite names it (figtree-latin-wght-normal-<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

/**
 * The console is for signed-in staff who passed the authenticator step. The smoke run has no API, so
 * the calls are answered by ./fake-api: the session refresh with a grant (signed in) or a 401
 * (signed out), `GET /v1/me` as a super admin, and every admin list as an empty page.
 */
test.beforeEach(async ({ page }, info) => {
  guard = await guardPage(page);
  await answerEmptyLists(page);
  await answerRefresh(page, true);
  await answerMe(page, ['super_admin']);
  // Without the session hint a cold start skips the refresh and opens sign-in (shared/auth), as for a
  // visitor; the signed-out tests keep it off, so no 401 reaches the console.
  if (!info.title.startsWith('signed out')) {
    await page.addInitScript(() => localStorage.setItem('todayz.hasSession.admin', '1'));
  }
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
  await answer(page, '/v1/me', 401, problem(401, 'unauthorized'));
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
