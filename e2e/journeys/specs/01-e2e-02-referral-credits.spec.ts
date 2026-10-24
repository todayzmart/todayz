import { Customer, expect, placeAndCollect, Shop, test, waitForCredits } from '../../fixtures/journey';
import { ASHA_REFERRAL_CODE, SL2_ITEMS, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-02: a referred customer with 100 credits → order ₹300 using 30 → collected → 5% cashback 13 →
// wallet 83 → the shop's credit account +30 −13. A new customer claims Asha's code; the reward unlocks
// with the first collected order (01 §8). That first order is at Sri Lakshmi 2 with its cashback set
// to 0 for the order, so the wallet holds exactly the 100 the journey starts from.
test('E2E-02 referral credits, ₹300 order using 30, cashback 13, wallet 83, credit account +30 −13', async ({ who }) => {
  const referee = new Customer(await who('referee'));
  const owner = await who('owner');
  const sl = new Shop(owner, STORES.SL);
  const sl2 = new Shop(owner, STORES.SL2);

  await test.step('the new customer claims a friend’s referral code', async () => {
    const claim = await referee.api.ok<{ kind: string; status: string; rewardCredits: number }>('claim the referral', 'POST', '/v1/me/referrals/claim', {
      code: ASHA_REFERRAL_CODE,
      referrer: 'customer',
    });
    expect(claim.kind).toBe('customer_customer');
    expect(claim.rewardCredits).toBe(100);
  });

  await test.step('the first collected order unlocks the 100 credits', async () => {
    await sl2.withSettings({ cashbackBp: 0 }, () => placeAndCollect(referee, sl2, [{ storeItemId: SL2_ITEMS.tea.id, quantity: 1 }]));
    await waitForCredits(referee, 100);
  });

  const order = await test.step('a ₹300 order using 30 credits is collected', async () => {
    const q = await referee.quote(STORES.SL, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 3 }], 30);
    expect(q.creditsAllowed, 'checkout allows 30 credits').toBe(30);
    expect(q.totalPaise, '₹300 less ₹30 of credits').toBe(27000);
    const o = await placeAndCollect(referee, sl, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 3 }], 30);
    expect(o.creditsUsed).toBe(30);
    expect(o.cashbackCredits, '5% of ₹270, rounded down').toBe(13);
    return o;
  });

  await test.step('the wallet shows 83', async () => {
    await waitForCredits(referee, 83);
  });

  await test.step('the shop’s credit account shows +30 for the spend and −13 for the cashback', async () => {
    await expect
      .poll(async () => {
        const acct = await sl.creditAccount();
        const lines = acct.lines.filter((l) => l.orderNumber === order.number);
        return lines.map((l) => `${l.kind}:${l.credits}`).sort();
      })
      .toEqual(['cashback:-13', 'spend_capture:30']);
  });
});
