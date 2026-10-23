import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { blockTurnstile, TURNSTILE_URL } from './helpers.ts';

/** The self-hosted Figtree file, as Astro names it (figtree-latin-wght-normal.<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await blockTurnstile(page);
});

test.afterEach(() => {
  // The only non-local request the site may ever try is the Turnstile script on lead-form pages,
  // and blockTurnstile() answers it before the guard sees it.
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

for (const [name, viewport] of Object.entries(VIEWPORTS)) {
  test(`${name}: home renders with self-hosted fonts`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en-IN');
    await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
    await shot(page, `site-${name}.png`, { fullPage: true });
  });
}

test('icons are served locally', async ({ request }) => {
  for (const icon of ['favicon-32.png', 'apple-touch-icon.png', 'mark-96.png']) {
    const res = await request.get(`/icons/${icon}`);
    expect(res.status(), icon).toBe(200);
    expect(res.headers()['content-type']).toContain('image/png');
  }
});

test('the only scripts a page asks for are its own hashed bundle and Turnstile', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', (r) => {
    if (r.resourceType() === 'script') scripts.push(r.url());
  });
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  const origin = new URL(page.url()).origin;
  for (const url of scripts) {
    expect(url === TURNSTILE_URL || (url.startsWith(`${origin}/_astro/`) && url.endsWith('.js')), url).toBe(true);
  }
});
