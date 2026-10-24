import { Customer, expect, test, waitForCredits, type Order } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-12: a super admin changes `credit_max_per_order` 30 → 40 with step-up → the next checkout allows
// 40; the audit entry is visible. There is no admin settings API at this gate (A-17 and its API come
// with W3-09/W3-05), so only the "before" half runs: 30 credits are used, 40 are refused.
//
// More credits than `credit_max_per_order` is a 409 `credit_max_exceeded` (04-api, 08 CR-02; the
// lead's answer to Q-1 in docs/qa/W2-13/REPORT.md; D-216, W2-02 finish).
test('E2E-12 before the change: 30 credits are used, 40 are refused with credit_max_exceeded', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const credits = (await c.wallet()).availableCredits;
  test.skip(credits < 40, 'the referee needs 40 credits for this check (E2E-02 and E2E-06 run first)');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 5 }];

  await test.step('30 credits: quoted, placed, held, and given back on cancel', async () => {
    const q = await c.quote(STORES.SL, items, 30);
    expect(q.creditsAllowed, 'checkout allows at most 30').toBe(30);
    expect(q.creditsUsed).toBe(30);
    expect(q.totalPaise).toBe(50000 - 3000);
    const placed = await c.api.post<Order>('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 30, expectedTotalPaise: q.totalPaise });
    expect(placed.status, `order with 30 credits: ${placed.code}`).toBe(201);
    expect(placed.body.creditsUsed, '30 credits are held').toBe(30);
    await c.api.ok('customer cancels the order', 'POST', `/v1/me/orders/${placed.body.id}/cancel`, { reason: 'changed_mind' });
    await waitForCredits(c, credits);
  });

  await test.step('40 credits: refused with credit_max_exceeded, nothing held', async () => {
    const r = await c.api.post<Order>('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 40, expectedTotalPaise: 50000 - 4000 });
    expect(r.status, `order with 40 credits: ${r.code}`).toBe(409);
    expect(r.code).toBe('credit_max_exceeded');
    await waitForCredits(c, credits);
  });
});

test.fixme('E2E-12 super admin sets credit_max_per_order to 40 with step-up; checkout allows 40; audit entry visible', async () => {
  // Deferred: no admin settings API or A-17 screen yet (W3-09 / W3-05); the W3 gate runs this.
});
