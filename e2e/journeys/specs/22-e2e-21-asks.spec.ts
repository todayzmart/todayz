import { Customer, expect, Shop, test, type Order, type Quote } from '../../fixtures/journey';
import { ANNAPURNA, QA_POINT, STORES } from '../../fixtures/seed';

// E2E-21 (D-101, ASK-*): the customer searches "LED bulb 9 W", the only listing is at Annapurna and is
// five days old (stock not confirmed), so they ask nearby. The shops in the distance band get the ask
// (B-20 shows the distance band only). Here the replies come from the seed's signed-in staff: Sri
// Lakshmi 2's manager says Yes (no listing, so no price) and Sri Lakshmi's cashier says No. The customer
// sees the live progress. Then Annapurna's owner says Yes at ₹100 (below the ₹110 listing), the customer
// books that reply, AP accepts and the customer collects it with the code. Nandi Hardware mutes asks in
// the seed, so it never gets one.
interface Search {
  results: { store: { storeId: string; storeSlug: string }; items: { storeItemId: string; productId: string; name: string; confirmedAt: string | null }[] }[];
}
interface Ask {
  ask: { id: string; progress: { asked: number; replied: number; bestPricePaise: number | null }; yesCount: number };
  replies: { askReplyId: string; storeId: string; storeItemId: string | null; pricePaise: number | null; bookable: boolean; orderId: string | null }[];
}
interface ShopAsks {
  items: { id: string }[];
}
interface PricedLine {
  storeItemId: string;
  unitPricePaise: number;
  priceSource: string;
}

test('E2E-21 stale result → ask nearby → shops reply → book AP’s Yes at ₹100 → collect with the code', async ({ who }) => {
  const c = new Customer(await who('delivery'));
  const manager = await who('manager');
  const cashier = await who('cashier');
  const sl = new Shop(cashier, STORES.SL);
  const sl2 = new Shop(manager, STORES.SL2);
  const ap = new Shop(await who('apOwner'), STORES.AP, ANNAPURNA.id);

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

  await test.step('the ask reached the shops in the band: Annapurna, Sri Lakshmi and Sri Lakshmi 2', async () => {
    const mine = async (shop: Shop) => (await shop.api.ok<ShopAsks>('shop lists open asks', 'GET', `${shop.base}/asks`, undefined, { params: { status: 'open' } })).items.some((i) => i.id === askId);
    expect(await mine(sl), 'Sri Lakshmi got it').toBe(true);
    expect(await mine(sl2), 'Sri Lakshmi 2 got it').toBe(true);
    expect(await mine(ap), 'Annapurna got it').toBe(true);
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.ask.progress.asked, 'at least the three unmuted shops in the band').toBeGreaterThanOrEqual(3);
    expect(view.ask.progress.replied).toBe(0);
  });

  await test.step('Sri Lakshmi 2’s manager says Yes with no listing; Sri Lakshmi’s cashier says No', async () => {
    await sl2.api.ok('manager says yes', 'POST', `${sl2.base}/asks/${askId}/reply`, { answer: 'yes' });
    await sl.api.ok('cashier says no', 'POST', `${sl.base}/asks/${askId}/reply`, { answer: 'no' });
  });

  await test.step('the customer sees the progress: two replied, one Yes, no price to book yet', async () => {
    await expect
      .poll(async () => (await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`)).ask.progress.replied, { message: 'two replies counted', timeout: 20_000 })
      .toBe(2);
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.ask.yesCount).toBe(1);
    expect(view.replies.some((r) => r.bookable), 'a Yes with no price cannot be booked').toBe(false);
  });

  const reply = await test.step('Annapurna’s owner says Yes at ₹100: the customer can book that reply', async () => {
    await ap.api.ok('AP owner says yes at a price', 'POST', `${ap.base}/asks/${askId}/reply`, { answer: 'yes', pricePaise: 10000 });
    let found: Ask['replies'][number] | undefined;
    await expect
      .poll(
        async () => {
          const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
          found = view.replies.find((r) => r.storeId === STORES.AP.id);
          return found?.bookable ?? false;
        },
        { message: 'AP’s reply is bookable', timeout: 20_000 },
      )
      .toBe(true);
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.ask.yesCount).toBe(2);
    expect(view.ask.progress.bestPricePaise).toBe(10000);
    expect(found?.pricePaise).toBe(10000);
    expect(found?.storeItemId, 'the reply names AP’s listing').toBeTruthy();
    return found as Ask['replies'][number];
  });

  const placed = await test.step('the customer books the reply: the line is priced at ₹100 from the ask', async () => {
    const cart = { storeId: STORES.AP.id, fulfilment: 'pickup', items: [{ storeItemId: reply.storeItemId, quantity: 1 }], creditsToUse: 0, askReplyId: reply.askReplyId };
    const q = await c.api.ok<Quote & { lines: PricedLine[] }>('customer prices the reply', 'POST', '/v1/me/checkout/quote', cart);
    expect(q.orderable, 'the reply can be booked').toBe(true);
    expect(q.lines[0]?.priceSource).toBe('ask_reply');
    expect(q.lines[0]?.unitPricePaise).toBe(10000);
    const o = await c.api.ok<Order>('customer books the reply', 'POST', '/v1/me/orders', { ...cart, expectedTotalPaise: q.totalPaise });
    expect(o.status).toBe('placed');
    expect(o.lines[0]?.unitPricePaise).toBe(10000);
    return o;
  });

  await test.step('Annapurna accepts and readies it; the customer collects it with the code', async () => {
    await ap.accept(placed.id);
    await ap.ready(placed.id);
    const done = await ap.handOver(placed.id, await c.code(placed.id));
    expect(done.status, 'the order is collected').toBe('collected');
    const view = await c.api.ok<Ask>('customer reads the ask', 'GET', `/v1/me/asks/${askId}`);
    expect(view.replies.find((r) => r.askReplyId === reply.askReplyId)?.orderId, 'the reply points at the order').toBe(placed.id);
  });
});
