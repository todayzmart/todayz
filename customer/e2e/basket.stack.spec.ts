import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// W2-14 stack journey (C-22, D-110). Needs `make dev` on a fresh `make reset`: the seed gives Asha two
// baskets, "Weekly groceries" (Sri Lakshmi has all of it) and "Sunday breakfast" (split between Sri Lakshmi
// and Sri Lakshmi 2). The code comes from the mocks server; nothing is printed.
//   pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts basket.stack.spec.ts
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';
const PHONE = process.env.TZ_BASKET_PHONE ?? '9000000001'; // national number of the seed customer Asha
const SLUG = process.env.TZ_SHOP_STORE_SLUG ?? 'sri-lakshmi-indiranagar';

/** Saves a screenshot for the QA pack when TZ_QA_DIR is set (synthetic seed data only). */
async function shot(page: Page, name: string) {
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/${name}-390.png` });
}

async function latestOtp(request: APIRequestContext, national: string, since: string): Promise<string> {
  const r = await request.get(`${MOCKS}/_mock/sms/latest-otp`, { params: { to: `+91${national}`, since, wait: '8' } });
  expect(r.status(), 'the mocks server returned a code').toBe(200);
  return ((await r.json()) as { otp: string }).otp;
}

async function customerSignIn(page: Page, request: APIRequestContext, phone: string) {
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const code = await latestOtp(request, phone, since);
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(code);
  await expect(page).not.toHaveURL(/\/sign-in/);
}

test('saved baskets: one shop has everything, fill the cart; a two-shop split; add from a shop', async ({ page, request }) => {
  // C-01 and C-02 once: a language and a PIN code in the seeded area.
  await page.goto('/');
  await page.getByRole('button', { name: 'English' }).click();
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('textbox', { name: 'PIN code' }).fill('560038');
  await page.getByRole('button', { name: 'Use this PIN code' }).click();
  await expect(page.getByRole('link', { name: /Baskets/ })).toBeVisible();

  await page.goto('/sign-in?next=%2Fbaskets');
  await customerSignIn(page, request, PHONE);
  await expect(page).toHaveURL(/\/baskets$/);
  await expect(page.getByRole('link', { name: 'Weekly groceries' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sunday breakfast' })).toBeVisible();
  await shot(page, 'basket-list');

  // "Weekly groceries": Sri Lakshmi has all four lines; Fill cart lands on the cart with that shop.
  await page.getByRole('link', { name: 'Weekly groceries' }).click();
  await expect(page).toHaveURL(/\/baskets\/[0-9A-Z]{26}$/);
  await page.getByRole('button', { name: 'Search shops' }).click();
  await expect(page.getByText(/Checked \d+ shops nearby/)).toBeVisible();
  await expect(page.getByText('Has everything').first()).toBeVisible();
  await page.getByText(/Checked \d+ shops nearby/).scrollIntoViewIfNeeded();
  await page.evaluate(() => window.scrollBy(0, 120));
  await shot(page, 'basket-results');
  await page.getByRole('button', { name: 'Fill cart' }).first().click();
  await expect(page).toHaveURL(/\/cart$/);
  await expect(page.getByText(/Sri Lakshmi/).first()).toBeVisible();
  await shot(page, 'basket-cart');

  // "Sunday breakfast": no single shop has it all, so the split card names both shops.
  await page.goto('/baskets');
  await page.getByRole('link', { name: 'Sunday breakfast' }).click();
  await page.getByRole('button', { name: 'Search shops' }).click();
  await expect(page.getByRole('heading', { name: 'Get it all from two shops' })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Fill cart from Sri Lakshmi/ })).toBeVisible();
  await page.getByRole('heading', { name: 'Get it all from two shops' }).evaluate((h) => h.scrollIntoView({ block: 'start' }));
  await page.evaluate(() => window.scrollBy(0, -16));
  await shot(page, 'basket-split');

  // "Add to basket" on a shop's item card: pick a saved basket in the sheet.
  await page.goto(`/s/${SLUG}`);
  await page.getByRole('button', { name: /^Add to basket: / }).first().click();
  await page.getByRole('button', { name: /Sunday breakfast/ }).click();
  await expect(page.getByText(/Added .* to Sunday breakfast/)).toBeVisible();
  await shot(page, 'basket-add');
});
