import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';

/** The self-hosted Figtree file, as Astro names it (figtree-latin-wght-normal.<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

for (const [name, viewport] of Object.entries(VIEWPORTS)) {
  test(`${name}: home renders with self-hosted fonts and no scripts`, async ({ page }) => {
    const scripts: string[] = [];
    page.on('request', (r) => {
      if (r.resourceType() === 'script') scripts.push(r.url());
    });
    await page.setViewportSize(viewport);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en-IN');
    await expect(page.locator('script')).toHaveCount(0);
    await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
    expect(scripts).toEqual([]);
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
