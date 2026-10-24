import { Customer, checkoutCart, customerFirstRun, expect, openApp, Shop, test, URLS } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-13 (D-110, non-AI part): saved baskets → basket search → Fill cart → order → pickup, then a
// basket no one shop has in full → the split → first order → "Next: the rest from {shop}" → second
// order. Asha's two seeded baskets are used: "Weekly groceries" (Sri Lakshmi has all of it) and
// "Sunday breakfast" (Sri Lakshmi and Sri Lakshmi 2). The shop takes each order through the API.

async function collect(c: Customer, shop: Shop, id: string) {
  await shop.accept(id);
  await shop.ready(id);
  const done = await shop.handOver(id, await c.code(id));
  expect(done.status).toBe('collected');
}

test('E2E-13 a basket one shop has in full → Fill cart → order → pickup; a split basket → two orders', async ({ who, browser }) => {
  const asha = new Customer(await who('asha'));
  const owner = await who('owner');
  const sl = new Shop(owner, STORES.SL);
  const sl2 = new Shop(owner, STORES.SL2);
  const app = await openApp(browser, asha.who, URLS.customer);
  const { page } = app;
  try {
    await customerFirstRun(page);

    await test.step('"Weekly groceries": a shop has everything → Fill cart → order → pickup', async () => {
      await page.goto('/baskets');
      await page.getByRole('link', { name: 'Weekly groceries' }).click();
      await page.getByRole('button', { name: 'Search shops' }).click();
      await expect(page.getByText('Has everything').first()).toBeVisible();
      await page.getByRole('button', { name: 'Fill cart' }).first().click();
      await expect(page).toHaveURL(/\/cart$/);
      const id = await checkoutCart(page);
      await collect(asha, sl, id);
      await expect(page.getByText('Order complete')).toBeVisible({ timeout: 20_000 });
    });

    await test.step('"Sunday breakfast": the split card → first order → Next: the rest → second order', async () => {
      await page.goto('/baskets');
      await page.getByRole('link', { name: 'Sunday breakfast' }).click();
      await page.getByRole('button', { name: 'Search shops' }).click();
      await expect(page.getByRole('heading', { name: 'Get it all from two shops' })).toBeVisible();
      await page.getByRole('button', { name: /^Fill cart from Sri Lakshmi(?! 2)/ }).click();
      await expect(page).toHaveURL(/\/cart$/);
      const first = await checkoutCart(page);
      await expect(page.getByText('Next: the rest from Sri Lakshmi 2')).toBeVisible();
      await collect(asha, sl, first);

      await page.getByRole('button', { name: 'Fill cart from Sri Lakshmi 2' }).click();
      await expect(page).toHaveURL(/\/cart$/);
      const second = await checkoutCart(page);
      expect((await asha.order(second)).lines.length).toBeGreaterThan(0);
      await collect(asha, sl2, second);
    });
  } finally {
    await app.close();
  }
});

/**
 * Adds the first item on the page to Sunday breakfast and waits for the toast. Result cards and shop
 * pages use the compact button ("Add to basket: {name}"); the product page's is just "Add to basket".
 */
async function addFirstToBasket(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: /^Add to basket(: |$)/ }).first().click();
  await page.getByRole('button', { name: /Sunday breakfast/ }).click();
  await expect(page.getByText(/Added .* to Sunday breakfast/)).toBeVisible();
}

test('E2E-13 build a basket from search results (C-05 → Add to basket)', async ({ who, browser }) => {
  const app = await openApp(browser, await who('asha'), URLS.customer);
  try {
    await customerFirstRun(app.page);
    await app.page.goto('/search?q=milk');
    await addFirstToBasket(app.page);
  } finally {
    await app.close();
  }
});

for (const [name, path] of [
  ['a shop page (C-07)', `/s/${STORES.SL.slug}`],
  ['a product page (C-08)', `/s/${STORES.SL.slug}/i/${SL_ITEMS.milk.id}`],
] as const) {
  test(`E2E-13 build a basket from ${name}`, async ({ who, browser }) => {
    const app = await openApp(browser, await who('asha'), URLS.customer);
    try {
      await customerFirstRun(app.page);
      const store = app.page.waitForResponse((r) => new URL(r.url()).pathname === `/v1/discover/stores/${STORES.SL.slug}`);
      await app.page.goto(path);
      expect((await store).status(), 'GET /v1/discover/stores/{slug}, which the page loads first').toBe(200);
      await addFirstToBasket(app.page);
    } finally {
      await app.close();
    }
  });
}
