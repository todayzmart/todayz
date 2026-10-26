import { Customer, customerFirstRun, expect, openApp, placeAndCollect, Shop, test, URLS, waitForCredits } from '../../fixtures/journey';
import { QA_POINT, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-01: the QR standee at the counter → the shopper scans it, searches, books a pickup order,
// collects it with the code, reviews the shop → the store referral qualifies: the shopper gets 100
// credits and the store's credit account gets +30 (01 §8). The standee's counts read 1 scan, 1 sign-up,
// 1 qualified. The customer app has no claim screen and no review screen yet, so those two steps use
// the API (a claim without a code uses the attribution cookie the scan set in the same browser).
interface Standee {
  id: string;
  link: string;
  counts: { scans: number; signUps: number; qualified: number };
}
interface Search {
  results: { store: { storeId: string; storeSlug: string }; items: { storeItemId: string; name: string }[] }[];
}

test('E2E-01 standee scan → search → pickup order → collect → review → store referral +30', async ({ who, browser }) => {
  const c = new Customer(await who('standee'));
  const owner = await who('owner');
  const sl = new Shop(owner, STORES.SL);
  const standeesPath = `${sl.base}/standees`;

  const standee = await test.step('the shop makes a standee for the billing counter in B-10', async () => {
    return owner.api.ok<Standee>('owner adds a standee', 'POST', standeesPath, { campaign: 'E2E-01 counter' });
  });

  const app = await openApp(browser, c.who, URLS.customer);
  const { page } = app;
  try {
    await test.step('the shopper scans the QR: the link opens the shop page in the customer app', async () => {
      await customerFirstRun(page);
      const link = new URL(standee.link);
      await page.goto(`${URLS.api}${link.pathname}${link.search}`);
      await expect(page).toHaveURL(new RegExp(`/s/${STORES.SL.slug}`));
      await expect(page.getByRole('heading', { level: 1, name: STORES.SL.name })).toBeVisible();
    });

    await test.step('the shopper signs up through the store referral (the scan’s cookie, no code typed)', async () => {
      const claim = await c.api.ok<{ kind: string; rewardCredits: number }>('claim the standee referral', 'POST', '/v1/me/referrals/claim', {});
      expect(claim.kind).toBe('retailer_customer');
      expect(claim.rewardCredits).toBe(100);
    });

    await test.step('the shopper searches and finds the item at Sri Lakshmi', async () => {
      const found = await c.api.ok<Search>('customer searches', 'GET', '/v1/discover/search', undefined, {
        params: { q: 'qa pack', lat: QA_POINT.lat, lng: QA_POINT.lng },
      });
      const items = found.results.filter((r) => r.store.storeId === STORES.SL.id).flatMap((r) => r.items.map((i) => i.storeItemId));
      expect(items, 'the ₹100 pack is in the results').toContain(SL_ITEMS.qa100.id);
    });
  } finally {
    await app.close();
  }

  const order = await test.step('a ₹100 pickup order is booked, accepted, readied and collected with the code', async () => {
    // Cashback off for the order, so the wallet holds exactly the reward.
    return sl.withSettings({ cashbackBp: 0 }, () => placeAndCollect(c, sl, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }]));
  });

  await test.step('the shopper reviews the shop', async () => {
    await c.api.ok('customer reviews the order', 'POST', '/v1/me/reviews', { orderId: order.id, stars: 5, text: 'E2E-01 journey review', tags: [] });
  });

  await test.step('the referral qualifies: 100 credits for the shopper, +30 on the store’s account', async () => {
    await waitForCredits(c, 100);
    await expect
      .poll(async () => (await sl.creditAccount()).lines.filter((l) => l.kind === 'referral_reward' && l.credits === 30).length, { message: 'the store has a +30 referral line', timeout: 30_000 })
      .toBeGreaterThan(0);
  });

  await test.step('B-10 counts: 1 scan, 1 sign-up, 1 qualified', async () => {
    await expect
      .poll(
        async () => {
          const list = await owner.api.ok<{ items: Standee[] }>('owner lists the standees', 'GET', standeesPath);
          return list.items.find((s) => s.id === standee.id)?.counts;
        },
        { timeout: 30_000 },
      )
      .toEqual({ scans: 1, signUps: 1, qualified: 1 });
  });
});
