import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';

/** The self-hosted Figtree file, as Vite names it (figtree-latin-wght-normal-<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

/**
 * The app is for signed-in staff. The smoke run has no API, so the session refresh is answered here:
 * with a grant (signed in) or a 401 (signed out). The API is on another origin, so the cookie-carrying
 * call needs the preflight and an explicit origin in the answer.
 */
async function answerRefresh(page: Page, signedIn: boolean) {
  await page.route('**/v1/auth/refresh', (route) => {
    const origin = route.request().headers()['origin'] ?? '*';
    const cors = {
      'access-control-allow-origin': origin,
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'content-type, x-tz-client, accept-language',
      'access-control-allow-methods': 'POST, OPTIONS',
    };
    if (route.request().method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors });
    if (!signedIn) {
      return route.fulfill({
        status: 401,
        contentType: 'application/problem+json',
        headers: cors,
        body: JSON.stringify({ type: 'about:blank#unauthorized', title: 'Unauthorized', status: 401, code: 'unauthorized' }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: cors,
      body: JSON.stringify({
        accessToken: 'smoke-token',
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        tokenType: 'Bearer',
        userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
      }),
    });
  });
}

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await answerRefresh(page, true);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const sidebar = '.tz-side';
const tabs = '.tz-bnav';

test('phone: lands on Overview with bottom tabs, no sidebar (B-03)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
  await expect(page.locator(tabs)).toBeVisible();
  await expect(page.locator(sidebar)).toBeHidden();
  await expect(page.locator(tabs).getByRole('link')).toHaveCount(5);
  await expect(page.locator(tabs).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
  await expect(page.locator(tabs).getByRole('link', { name: 'Orders' })).not.toHaveAttribute('aria-current');
  await expect(page.getByRole('main')).toHaveAttribute('data-density', 'touch');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-phone.png');
});

test('desktop: sidebar with Overview active, no bottom tabs', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator(sidebar)).toBeVisible();
  await expect(page.locator(tabs)).toBeHidden();
  await expect(page.locator(sidebar).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('main')).not.toHaveAttribute('data-density');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-desktop.png');
});

test('Back after the /overview redirect leaves the app instead of bouncing', async ({ page }) => {
  await page.goto('about:blank');
  await page.goto('/overview');
  await expect(page).toHaveURL(/\/$/);
  await page.goBack();
  await expect(page).toHaveURL('about:blank');
});

test('the Overview tab goes back to / from another section', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.locator(tabs).getByRole('link', { name: 'Overview' }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
});

test('More tab lists the other sections', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.locator(tabs).getByRole('link', { name: 'More' }).click();
  await expect(page).toHaveURL(/\/more$/);
  await page.getByRole('region', { name: 'More' }).getByRole('link', { name: 'Offers' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Offers' })).toBeVisible();
});

test('pseudo-locale via ?locale=pseudo survives the /overview redirect', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/overview?locale=pseudo');
  await expect(page).toHaveURL(/\/\?locale=pseudo$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/^\[.+\]$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-pseudo-desktop.png');
  await page.setViewportSize(VIEWPORTS.phone);
  await shot(page, 'business-pseudo-phone.png');
});

test('PWA manifest and icons are served locally', async ({ page, request }) => {
  await page.goto('/');
  const manifest = await (await request.get('/manifest.webmanifest')).json();
  expect(manifest).toMatchObject({ name: 'TodayZ Business', lang: 'en-IN', display: 'standalone' });
  for (const icon of manifest.icons as { src: string }[]) {
    expect((await request.get(icon.src)).status(), icon.src).toBe(200);
  }
});

test('signed out: any page opens the sign-in screen (B-01), with no sidebar or tabs', async ({ page }) => {
  await page.unroute('**/v1/auth/refresh');
  await answerRefresh(page, false);
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Forders$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Business' })).toBeVisible();
  await expect(page.getByLabel('Mobile number')).toBeVisible();
  await expect(page.locator(tabs)).toHaveCount(0);
  await expect(page.locator(sidebar)).toHaveCount(0);
  await shot(page, 'business-sign-in-phone.png');
});
