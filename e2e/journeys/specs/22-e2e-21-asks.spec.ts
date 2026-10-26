import { Customer, expect, Shop, test } from '../../fixtures/journey';
import { QA_POINT, STORES } from '../../fixtures/seed';

// E2E-21 (D-101, ASK-*): the customer searches "LED bulb 9 W", the only listing is at Annapurna and is
// five days old (stock not confirmed), so they ask nearby. The shops in the distance band get the ask
// (B-20 shows the distance band only). Here the replies come from the seed's signed-in staff: Sri
// Lakshmi 2's manager says Yes (no listing, so no price) and Sri Lakshmi's cashier says No. The customer
// sees the live progress.
// The seed has no staff for Annapurna (AP) or Nandi Hardware (NH): their owner cannot sign in, so AP's Yes at
// ₹100 and the booking of it cannot run yet (second test, fixme).
interface Search {
  results: { store: { storeId: string; storeSlug: string }; items: { storeItemId: string; productId: string; name: string; confirmedAt: string | null }[] }[];
}
interface Ask {
  ask: { id: string; progress: { asked: number; replied: number; bestPricePaise: number | null }; yesCount: number };
  replies: { askReplyId: string; storeId: string; pricePaise: number | null; bookable: boolean }[];
}
interface ShopAsks {
  items: { id: string }[];
}

test('E2E-21 search shows a stale result → ask nearby → shops reply → the customer sees the progress', async ({ who }) => {
  const c = new Customer(await who('delivery'));
  const owner = await who('owner');
  const manager = await who('manager');
  const cashier = await who('cashier');
  const sl = new Shop(cashier, STORES.SL);
  const sl2 = new Shop(manager, STORES.SL2);

  const found = await test.step('the customer searches "LED bulb 9 W": the result is not confirmed recently', async () => {
    const r = await c.api.ok<Search>('customer searches', 'GET', '/v1/discover/search', undefined, { params: { q: 'LED bulb 9 W', lat: QA_POINT.lat, lng: QA_POINT.lng } });
    const item = r.results.flatMap((x) => x.items).find((i) => /LED bulb/i.test(i.name));
    expect(item, 'the bulb is in the results').toBeTruthy();
    expect(item?.confirmedAt, 'its stock is stale').toBeNull();
    return item;
  });

  const askId = await test.step('the customer asks nearby shops', async () => {
    const a = await c.api.ok<Ask['ask']>('customer asks', 'POST', '/v1/me/asks', { productId: found?.productId, quantity: 1, needBy: 'today', lat: QA_POINT.lat, lng: QA_POINT.lng });
    return a.id;
  });

  await test.step('the ask reached the shops in the band, Sri Lakshmi and Sri Lakshmi 2 among them', async () => {
    const mine = async (shop: Shop) => (await shop.api.ok<ShopAsks>('shop lists open asks', 'GET', `${shop.base}/asks`, undefined, { params: { status: 'open' } })).items.some((i) => i.id === askId);
    expect(await mine(sl), 'Sri Lakshmi got it').toBe(true);
    expect(await mine(sl2), 'Sri Lakshmi 2 got it').toBe(true);
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.ask.progress.asked, 'at least the four shops in the band').toBeGreaterThanOrEqual(4);
    expect(view.ask.progress.replied).toBe(0);
  });

  await test.step('Sri Lakshmi 2’s manager says Yes with no listing; Sri Lakshmi’s cashier says No', async () => {
    await sl2.api.ok('manager says yes', 'POST', `${sl2.base}/asks/${askId}/reply`, { answer: 'yes' });
    await sl.api.ok('cashier says no', 'POST', `${sl.base}/asks/${askId}/reply`, { answer: 'no' });
    expect(owner, 'the owner is signed in for the Wave 4 steps').toBeTruthy();
  });

  await test.step('the customer sees the progress: two replied, one Yes, no price to book yet', async () => {
    await expect
      .poll(async () => (await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`)).ask.progress.replied, { message: 'two replies counted', timeout: 20_000 })
      .toBe(2);
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.ask.yesCount).toBe(1);
    expect(view.replies.some((r) => r.bookable), 'a Yes with no price cannot be booked').toBe(false);
  });
});

test.fixme('E2E-21 AP’s owner replies Yes at ₹100, the customer books it, AP accepts, collect with the code', async () => {
  // The seed has no staff for Annapurna (AP) or Nandi Hardware (NH), so no one can sign in as AP's owner (NH also mutes asks).
});
