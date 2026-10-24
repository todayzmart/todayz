import { Customer, expect, Shop, test, ulid, waitForCredits } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-06: a walk-in at the POS pays with credits through the bill QR (D-07). The cashier rings
// ₹200 and shows the QR; the customer (the E2E-02 referee, who holds credits) scans it, picks
// credits and confirms; the cashier completes the sale linked to the claim. The QR token is read
// from the API and used, never printed.
test('E2E-06 POS walk-in with bill QR credits', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const counter = new Shop(await who('cashier'), STORES.SL);
  const before = (await c.wallet()).availableCredits;
  test.skip(before < 1, 'the referee has no credits: E2E-02 did not complete');

  const saleClientId = ulid();
  const claim = await counter.api.ok<{ id: string; token: string; status: string }>('cashier shows the bill QR', 'POST', `${counter.base}/bill-claims`, {
    saleClientId,
    amountPaise: 20000,
  });
  expect(claim.status).toBe('open');

  const credits = await test.step('the customer scans the QR and uses credits', async () => {
    const preview = await c.api.ok<{ maxCredits: number }>('customer previews the bill', 'POST', `/v1/me/bill-claims/${claim.token}/preview`, {}, { idem: false });
    const use = Math.min(30, preview.maxCredits);
    expect(use, 'credits can be used on a ₹200 bill').toBeGreaterThan(0);
    const confirmed = await c.api.ok<{ creditsUsed: number; payablePaise: number; status: string }>('customer confirms the bill', 'POST', `/v1/me/bill-claims/${claim.token}/confirm`, { creditsToUse: use });
    expect(confirmed.creditsUsed).toBe(use);
    expect(confirmed.payablePaise).toBe(20000 - use * 100);
    return use;
  });

  await test.step('the cashier completes the sale linked to the claim', async () => {
    const sale = await counter.api.ok<{ creditsUsed: number; cashbackCredits: number; totalPaise: number }>('cashier records the sale', 'POST', `${counter.base}/pos/sales`, {
      clientId: saleClientId,
      lines: [{ clientId: ulid(), storeItemId: SL_ITEMS.qa100.id, unitPricePaise: SL_ITEMS.qa100.pricePaise, quantity: 2 }],
      paymentMethod: 'cash',
      link: { method: 'bill_qr', claimId: claim.id },
    });
    expect(sale.creditsUsed).toBe(credits);
    expect(sale.totalPaise).toBe(20000 - credits * 100);
    expect(sale.cashbackCredits, '5% of what was paid, rounded down').toBe(Math.floor(((20000 - credits * 100) * 500) / 1_000_000));
    await waitForCredits(c, before - credits + sale.cashbackCredits);
  });

  await test.step('the claim is used and cannot pay twice', async () => {
    const got = await counter.api.ok<{ status: string }>('cashier reads the claim', 'GET', `${counter.base}/bill-claims/${claim.id}`);
    expect(got.status).toBe('used');
    const again = await c.api.post(`/v1/me/bill-claims/${claim.token}/confirm`, { creditsToUse: 1 });
    expect(again.code).toBe('claim_already_used');
  });
});
