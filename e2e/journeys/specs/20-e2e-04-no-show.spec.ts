import { Customer, expect, moveClock, Shop, test, waitForStatus } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-04: a pickup order that is never collected expires as a no-show with a strike; after three
// strikes the customer may hold only one open order (01 §6). The test clock moves past each 30-minute
// hold; the worker expires the order.
test('E2E-04 three no-shows → strikes → the open-order limit', async ({ who }) => {
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

  await test.step('one open order is allowed, a second is refused', async () => {
    const first = await c.place(STORES.SL, items);
    const q = await c.quote(STORES.SL, items);
    const second = await c.api.post('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 0, expectedTotalPaise: q.totalPaise });
    expect(second.status).toBe(422);
    expect(second.code).toBe('ordering_suspended');
    await c.api.ok('customer cancels the open order', 'POST', `/v1/me/orders/${first.id}/cancel`, { reason: 'changed_mind' });
  });
});

test.fixme('E2E-04 the limit banner shows in the customer app and the business app', async () => {
  // Deferred: the no-show banner (BK-16) is being built in W2-02 finish, not merged at this gate.
});
