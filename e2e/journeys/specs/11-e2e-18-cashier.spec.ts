import { Customer, expect, Shop, test } from '../../fixtures/journey';
import { LAKSHMI, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-18 (builder-written): a cashier of Sri Lakshmi hands over orders at the counter but cannot
// cancel them, change the shop's settings, or see the other shop.
test('E2E-18 cashier permissions: hand over yes; cancel, settings and the other shop no', async ({ who }) => {
  const asha = new Customer(await who('asha'));
  const owner = new Shop(await who('owner'), STORES.SL);
  const cashier = new Shop(await who('cashier'), STORES.SL);

  await test.step('the cashier sees Sri Lakshmi’s orders and not Sri Lakshmi 2’s', async () => {
    await cashier.api.ok('cashier reads the orders', 'GET', `${cashier.base}/orders`);
    const other = await cashier.api.get(`/v1/biz/businesses/${LAKSHMI.id}/stores/${STORES.SL2.id}/orders`);
    expect([403, 404]).toContain(other.status);
  });

  await test.step('the cashier cannot change the shop’s settings', async () => {
    const { etag } = await owner.settings();
    const r = await cashier.api.patch(`${cashier.base}/settings`, { cashbackBp: 0 }, { ifMatch: etag });
    expect(r.status).toBe(403);
  });

  await test.step('the cashier hands over an order with the customer’s code', async () => {
    const o = await asha.place(STORES.SL, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }]);
    await owner.accept(o.id);
    await cashier.ready(o.id);
    expect((await cashier.handOver(o.id, await asha.code(o.id))).status).toBe('collected');
  });

  await test.step('the cashier cannot cancel an order; the owner can', async () => {
    const o = await asha.place(STORES.SL, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }]);
    await owner.accept(o.id);
    const r = await cashier.api.post(`${cashier.base}/orders/${o.id}/cancel`, { reason: 'other', note: 'E2E-18 cashier check' });
    expect(r.status).toBe(403);
    await owner.api.ok('owner cancels', 'POST', `${owner.base}/orders/${o.id}/cancel`, { reason: 'other', note: 'E2E-18 clean-up' });
    expect((await asha.order(o.id)).status).toBe('cancelled');
  });
});
