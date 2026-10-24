import { Customer, expect, openApp, Shop, test, URLS, type Order } from '../../fixtures/journey';
import { LAKSHMI, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-03: the shop accepts part of an order → the customer sees the new total on C-12 and takes it →
// collected. The first test drives the shop through the API; the second through the business app's
// Accept part sheet (B-xx, AcceptPartSheet).
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
    const r = await shop.api.post<Order>(`${shop.base}/orders/${placed.id}/accept`, { lines });
    expect(r.status, `shop accepts part: ${r.code}`).toBe(200);
    expect(r.body.status).toBe('awaiting_customer');
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

test('E2E-03 shop side in the business app: Accept part → Send to customer', async ({ who, browser }) => {
  const c = new Customer(await who('partial'));
  const owner = await who('owner');
  const placed = await c.place(STORES.SL, [
    { storeItemId: SL_ITEMS.qa100.id, quantity: 2 },
    { storeItemId: SL_ITEMS.cola.id, quantity: 1 },
  ]);
  const app = await openApp(browser, owner, URLS.business);
  try {
    const { page } = app;
    await page.goto('/choose-shop');
    await page.getByRole('region', { name: `Stores of ${LAKSHMI.name}` }).getByRole('button', { name: STORES.SL.name, exact: true }).click();
    await page.goto(`/orders/${placed.id}`);
    await page.getByRole('button', { name: 'Accept part', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: `Accept part of ${placed.number}` });
    await expect(sheet).toBeVisible();
    await sheet.getByRole('button', { name: /^One less, QA pack ₹100/ }).click();
    await sheet.getByRole('button', { name: 'Send to customer' }).click();
    await expect(page.getByText(/^Waiting for .* to take the changed order or cancel it\.$/)).toBeVisible();
    const o = await c.order(placed.id);
    expect(o.status).toBe('awaiting_customer');
    expect(o.lines.find((l) => l.storeItemId === SL_ITEMS.qa100.id)?.quantityAccepted).toBe(1);
  } finally {
    await app.close();
    await c.api.ok('customer cancels the changed order', 'POST', `/v1/me/orders/${placed.id}/cancel`, { reason: 'changed_mind' });
  }
});
