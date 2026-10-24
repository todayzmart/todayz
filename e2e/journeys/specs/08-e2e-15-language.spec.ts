import { Customer, checkoutCart, customerFirstRun, expect, openApp, Shop, test, URLS } from '../../fixtures/journey';
import { STORES } from '../../fixtures/seed';

// E2E-15 (builder-written): the customer switches language in the middle of a journey. The cart
// survives the switch, the page language and the stored choice follow it, the account's
// preferredLang is saved, and checkout carries on to a collected order.
test('E2E-15 language switch mid-journey keeps the cart and finishes the order', async ({ who, browser }) => {
  const asha = new Customer(await who('asha'));
  const shop = new Shop(await who('owner'), STORES.SL);
  const app = await openApp(browser, asha.who, URLS.customer);
  const { page } = app;
  try {
    await customerFirstRun(page);
    await test.step('put an item in the cart from search', async () => {
      await page.goto('/search?q=milk');
      await page.getByRole('button', { name: /^Add, / }).first().click();
      await page.goto('/cart');
      await expect(page.getByRole('heading', { level: 1, name: 'Cart' })).toBeVisible();
    });
    const cartText = await page.locator('main').innerText();

    await test.step('switch to Hindi on C-01', async () => {
      await page.goto('/language');
      await page.getByRole('button', { name: /हिन्दी/ }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe('hi');
      expect(await page.evaluate(() => localStorage.getItem('todayz.lang'))).toBe('hi');
      await expect.poll(async () => ((await asha.me()) as { preferredLang?: string }).preferredLang).toBe('hi');
    });

    await test.step('the cart is still there after the switch and a reload', async () => {
      await page.goto('/cart');
      await page.reload();
      await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe('hi');
      const shopLine = cartText.split('\n').find((l) => /Sri Lakshmi|Nandini/.test(l)) ?? 'Nandini';
      await expect(page.locator('main').getByText(shopLine).first()).toBeVisible();
    });

    await test.step('checkout carries on in Hindi to a collected order', async () => {
      const id = await checkoutCart(page);
      const o = await asha.order(id);
      await shop.accept(id);
      await shop.ready(id);
      expect((await shop.handOver(id, await asha.code(id))).status).toBe('collected');
      expect(o.status).toBe('placed');
    });

    await test.step('switch back to English', async () => {
      await page.goto('/language');
      await page.getByRole('button', { name: /English/ }).click();
      await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe('en');
    });
  } finally {
    await app.close();
  }
});

test.fixme('E2E-15 the screens read in Hindi after the switch', async () => {
  // Deferred: shared/i18n/compiled/customer/hi.json carries the English text for every key (0 of the
  // catalogue translated); the Hindi copy is not written yet.
});
