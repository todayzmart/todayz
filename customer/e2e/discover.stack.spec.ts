import { expect, test } from '@playwright/test';

// W2-08 journey against `make dev` and the seeded Sri Lakshmi stores (01KE0S1W6W8ZV7T1C3A6MS0001 and …MS0002).
// Written for the seed; not run in the weekend slice (the brief forbids make dev).
// Run: pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts discover.stack.spec.ts
test('home, search atta, open a shop, add to cart', async ({ page }) => {
  // C-01 and C-02 once: a language and a PIN code in the seeded area.
  await page.goto('/');
  await page.getByRole('button', { name: 'English' }).click();
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('textbox', { name: 'PIN code' }).fill('560001');
  await page.getByRole('button', { name: 'Use this PIN code' }).click();

  // C-03: stats and shops near you, radius defaults to 2 km.
  await expect(page.getByRole('button', { name: '2 km' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('heading', { name: 'Shops near you' })).toBeVisible();

  // C-05: search "atta"; results are grouped by shop and carry the proof line.
  await page.getByRole('searchbox').fill('atta');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/search\?q=atta/);
  await expect(page.getByText(/Checked \d+ shops nearby/)).toBeVisible();
  await expect(page.getByText(/Sri Lakshmi/).first()).toBeVisible();

  // C-07: open the first shop, then C-08 is one tap away from its items.
  await page.getByRole('link', { name: /Sri Lakshmi/ }).first().click();
  await expect(page).toHaveURL(/\/s\/[a-z0-9-]+/);
  await expect(page.getByRole('heading', { level: 1, name: /Sri Lakshmi/ })).toBeVisible();

  // Add to cart: the cart bar appears with the shop's name.
  await page.getByRole('button', { name: /^Add/ }).first().click();
  await expect(page.getByRole('button', { name: /View cart/ })).toBeVisible();
});
