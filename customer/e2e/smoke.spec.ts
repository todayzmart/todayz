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

test('home renders on a phone with the bottom nav', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeVisible();
  const nav = page.getByRole('navigation', { name: 'Main' });
  await expect(nav.getByRole('link')).toHaveCount(5);
  await expect(nav.getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'customer-phone.png');
});

test('home on desktop', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'customer-desktop.png');
});

test('bottom nav opens a section placeholder', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Search' }).click();
  await expect(page).toHaveURL(/\/search$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Search' })).toBeVisible();
  await expect(page.getByText('Coming soon')).toBeVisible();
});

test('pseudo-locale via ?locale=pseudo', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/?locale=pseudo');
  const h1 = page.getByRole('heading', { level: 1 });
  await expect(h1).toHaveText(/^\[.+\]$/);
  await expect(h1).not.toHaveText(/Everything nearby/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'customer-pseudo-phone.png');
});

test('PWA manifest and icons are served locally', async ({ page, request }) => {
  await page.goto('/');
  const manifest = await (await request.get('/manifest.webmanifest')).json();
  expect(manifest).toMatchObject({ name: 'TodayZ', lang: 'en-IN', display: 'standalone' });
  for (const icon of manifest.icons as { src: string }[]) {
    expect((await request.get(icon.src)).status(), icon.src).toBe(200);
  }
});
