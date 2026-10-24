import { Customer, expect, openApp, Shop, test, URLS } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-03: the shop accepts part of an order → the customer sees the new total on C-12 and takes it →
// collected. The shop side is driven through the API: the business app's partial-accept screen is
// still being built (W2-02 finish).
test('E2E-03 partial accept → customer confirms the new total → collected', async ({ who, browser }) => {
  const c = new Customer(await who('partial'));
  const shop = new Shop(await who('owner'), STORES.SL);

  const placed = await c.place(STORES.SL, [
    { storeItemId: SL_ITEMS.qa100.id, quantity: 2 },
    { storeItemId: SL_ITEMS.cola.id, quantity: 1 },
  ]);
  expect(placed.totalPaise).toBe(24000);

  await test.step('the shop can give one QA pack instead of two', async () => {
    const lines = placed.lines.map((l) => ({ orderItemId: l.id, quantity: l.storeItemId === SL_ITEMS.qa100.id ? 1 : l.quantityRequested }));
    const o = await shop.accept(placed.id, lines);
    expect(o.status).toBe('awaiting_customer');
  });

  const app = await openApp(browser, c.who, URLS.customer);
  try {
    await test.step('C-12 shows what changed and the new total; the customer takes it', async () => {
      const { page } = app;
      await page.goto(`/orders/${placed.id}`);
      await expect(page.getByRole('heading', { name: 'Sri Lakshmi changed your order' })).toBeVisible();
      await expect(page.getByText('1 instead of 2')).toBeVisible();
      await page.getByRole('button', { name: /^Take it · ₹140/ }).click();
      await expect(page.getByRole('group', { name: 'Pickup code' })).toBeVisible();
    });
  } finally {
    await app.close();
  }

  await test.step('the shop hands it over and the order is collected at ₹140', async () => {
    const accepted = await c.order(placed.id);
    expect(accepted.status).toBe('accepted');
    expect(accepted.totalPaise).toBe(14000);
    await shop.ready(placed.id);
    const done = await shop.handOver(placed.id, await c.code(placed.id));
    expect(done.status).toBe('collected');
  });
});

test.fixme('E2E-03 shop side in the business app (partial accept on B-xx)', async () => {
  // Deferred: the business app's partial-accept UI is in W2-02 finish, not merged at this gate.
});
