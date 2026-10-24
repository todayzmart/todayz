import { Customer, expect, test, waitForCredits, type Order } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-12: a super admin changes `credit_max_per_order` 30 → 40 with step-up → the next checkout allows
// 40; the audit entry is visible. There is no admin settings API at this gate (A-17 and its API come
// with W3-09/W3-05), so only the "before" half runs: checkout never spends more than 30.
//
// The order API clamps a request for 40 to the 30 the quote allows (orders/create.go: credits come
// from the quote); with the total for 40 the order is refused as price_changed, so 40 is never spent.
// Whether 40 should instead be refused with credit_max_exceeded (08-testing CR-02 says so for the
// credit rules) is a question for the lead, recorded in docs/qa/W2-13/REPORT.md.
test('E2E-12 before the change: checkout caps credits at 30 and never spends 40', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const credits = (await c.wallet()).availableCredits;
  test.skip(credits < 40, 'the referee needs 40 credits for this check (E2E-02 and E2E-06 run first)');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 5 }];

  const q = await c.quote(STORES.SL, items, 40);
  expect(q.creditsAllowed, 'checkout allows at most 30').toBe(30);
  expect(q.creditsUsed).toBe(30);
  expect(q.totalPaise).toBe(50000 - 3000);

  const order = { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 40 };
  const for40 = await c.api.post('/v1/me/orders', { ...order, expectedTotalPaise: 50000 - 4000 });
  expect(for40.status, 'the total for 40 credits is refused').toBe(409);
  expect(for40.code).toBe('price_changed');

  const placed = await c.api.post<Order>('/v1/me/orders', { ...order, expectedTotalPaise: q.totalPaise });
  expect(placed.status, `order at the capped total: ${placed.code}`).toBe(201);
  expect(placed.body.creditsUsed, 'only 30 credits are held').toBe(30);
  await c.api.ok('customer cancels the order', 'POST', `/v1/me/orders/${placed.body.id}/cancel`, { reason: 'changed_mind' });
  await waitForCredits(c, credits);
});

test.fixme('E2E-12 super admin sets credit_max_per_order to 40 with step-up; checkout allows 40; audit entry visible', async () => {
  // Deferred: no admin settings API or A-17 screen yet (W3-09 / W3-05); the W3 gate runs this.
});
