import { Customer, customerFirstRun, expect, moveClock, openApp, Shop, test, URLS, waitForStatus } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-04: a pickup order that is never collected expires as a no-show with a strike; after three
// strikes the customer may hold only one open order (01 §6). The test clock moves past each 30-minute
// hold; the worker expires the order.
test('E2E-04 three no-shows → strikes → the open-order limit and its banner', async ({ who, browser }) => {
  const c = new Customer(await who('noShow'));
  const shop = new Shop(await who('owner'), STORES.SL);
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }];

  for (const n of [1, 2, 3]) {
    await test.step(`no-show ${n}: accepted, never collected, the hold runs out`, async () => {
      const o = await c.place(STORES.SL, items);
      const accepted = await shop.accept(o.id);
      expect(accepted.holdUntil, 'a pickup hold starts at accept').not.toBeNull();
      await moveClock({ advance: '31m' });
      const expired = await waitForStatus(c, o.id, ['expired']);
      expect(expired.statusReason).toBe('no_show');
    });
  }

  await test.step('the account now has the open-order limit', async () => {
    const me = await c.me();
    expect(me.customer?.activeOrderLimit?.reason).toBe('no_show_strikes');
    expect(me.customer?.activeOrderLimit?.maxActiveOrders).toBe(1);
  });

  await test.step('the customer app shows the limit banner on Home (C-21)', async () => {
    const app = await openApp(browser, c.who, URLS.customer);
    try {
      await customerFirstRun(app.page);
      const banner = app.page.getByRole('status').filter({ hasText: 'Ordering is limited for now' });
      await expect(banner).toBeVisible();
      await expect(banner).toContainText(/You missed several pickups, so until .+ you can have one open order at a time\./);
    } finally {
      await app.close();
    }
  });

  await test.step('one open order is allowed, a second is refused', async () => {
    // Priced before the first order: with one open order, the quote refuses too (orders.yaml, BK-16).
    const q = await c.quote(STORES.SL, items);
    const first = await c.place(STORES.SL, items);
    const quote2 = await c.api.post('/v1/me/checkout/quote', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 0 }, { idem: false });
    expect(quote2.status).toBe(409);
    expect(quote2.code).toBe('ordering_suspended');
    const second = await c.api.post('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 0, expectedTotalPaise: q.totalPaise });
    expect(second.status).toBe(409);
    expect(second.code).toBe('ordering_suspended');
    await c.api.ok('customer cancels the open order', 'POST', `/v1/me/orders/${first.id}/cancel`, { reason: 'changed_mind' });
  });
});
