import { expect, openApp, test, URLS } from '../../fixtures/journey';
import { LAKSHMI, STORES } from '../../fixtures/seed';

// E2E-17 (builder-written): an owner of a business with two shops sees and runs both.
test('E2E-17 multi-store owner sees both shops and can open each one’s orders', async ({ who, browser }) => {
  const owner = await who('owner');

  await test.step('the API lists both shops and each one’s orders', async () => {
    const list = await owner.api.ok<{ items: { id: string }[] }>('owner lists the stores', 'GET', `/v1/biz/businesses/${LAKSHMI.id}/stores`);
    const ids = list.items.map((s) => s.id);
    expect(ids).toEqual(expect.arrayContaining([STORES.SL.id, STORES.SL2.id]));
    for (const s of [STORES.SL, STORES.SL2]) {
      await owner.api.ok(`owner reads ${s.name}'s orders`, 'GET', `/v1/biz/businesses/${LAKSHMI.id}/stores/${s.id}/orders`);
    }
  });

  await test.step('the business app shop picker (B-01) shows both shops', async () => {
    const app = await openApp(browser, owner, URLS.business);
    try {
      await app.page.goto('/choose-shop');
      const region = app.page.getByRole('region', { name: `Stores of ${LAKSHMI.name}` });
      await expect(region).toBeVisible();
      await expect(region.getByText(STORES.SL.name, { exact: true })).toBeVisible();
      await expect(region.getByText(STORES.SL2.name, { exact: true })).toBeVisible();
    } finally {
      await app.close();
    }
  });
});
