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

const sidebar = '.tz-side';
const tabs = '.tz-bnav';

test('phone: lands on Orders with bottom tabs, no sidebar (D-84)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await expect(page).toHaveURL(/\/orders$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Orders' })).toBeVisible();
  await expect(page.locator(tabs)).toBeVisible();
  await expect(page.locator(sidebar)).toBeHidden();
  await expect(page.locator(tabs).getByRole('link')).toHaveCount(5);
  await expect(page.locator(tabs).getByRole('link', { name: 'Orders' })).toHaveAttribute('aria-current', 'page');
  await expect(page.locator(tabs).getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current');
  await expect(page.getByRole('main')).toHaveAttribute('data-density', 'touch');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-phone.png');
});

test('desktop: sidebar with Orders active, no bottom tabs', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page).toHaveURL(/\/orders$/);
  await expect(page.locator(sidebar)).toBeVisible();
  await expect(page.locator(tabs)).toBeHidden();
  await expect(page.locator(sidebar).getByRole('link', { name: 'Orders' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('main')).not.toHaveAttribute('data-density');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-desktop.png');
});

test('Back after the landing redirect leaves the app instead of bouncing', async ({ page }) => {
  await page.goto('about:blank');
  await page.goto('/');
  await expect(page).toHaveURL(/\/orders$/);
  await page.goBack();
  await expect(page).toHaveURL('about:blank');
});

test('Overview stays reachable as a placeholder', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await page.locator(tabs).getByRole('link', { name: 'Overview' }).click();
  await expect(page).toHaveURL(/\/overview$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Your shop on TodayZ' })).toBeVisible();
});

test('More tab lists the other sections', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.locator(tabs).getByRole('link', { name: 'More' }).click();
  await expect(page).toHaveURL(/\/more$/);
  await page.getByRole('region', { name: 'More' }).getByRole('link', { name: 'Offers' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Offers' })).toBeVisible();
});

test('pseudo-locale via ?locale=pseudo survives the landing redirect', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/?locale=pseudo');
  await expect(page).toHaveURL(/\/orders\?locale=pseudo$/);
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
