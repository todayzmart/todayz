import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';

/** The self-hosted Figtree file, as Vite names it (figtree-latin-wght-normal-<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
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
