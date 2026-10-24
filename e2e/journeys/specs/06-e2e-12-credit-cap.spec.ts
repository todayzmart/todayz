import { Customer, expect, test } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-12: a super admin changes `credit_max_per_order` 30 → 40 with step-up → the next checkout allows
// 40; the audit entry is visible. There is no admin settings API at this gate (A-17 and its API come
// with W3-09/W3-05), so only the "before" half runs: checkout caps credits at 30 and the server
// refuses 40.
test('E2E-12 before the change: checkout caps credits at 30 and refuses 40', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const credits = (await c.wallet()).availableCredits;
  test.skip(credits < 40, 'the referee needs 40 credits for this check (E2E-02 and E2E-06 run first)');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 5 }];

  const q = await c.quote(STORES.SL, items, 40);
  expect(q.creditsAllowed, 'checkout allows at most 30').toBe(30);

  const r = await c.api.post('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 40, expectedTotalPaise: 50000 - 4000 });
  expect(r.status).toBe(422);
  expect(r.code).toBe('credit_max_exceeded');
});

test.fixme('E2E-12 super admin sets credit_max_per_order to 40 with step-up; checkout allows 40; audit entry visible', async () => {
  // Deferred: no admin settings API or A-17 screen yet (W3-09 / W3-05); the W3 gate runs this.
});
