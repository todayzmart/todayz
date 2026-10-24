import { request as playwrightRequest } from '@playwright/test';

import { anon } from '../../fixtures/api';
import { Customer, expect, test } from '../../fixtures/journey';
import { QA_POINT, seedItemId, STORES } from '../../fixtures/seed';

// E2E-20 (builder-written): a suspended shop is not found by customers and takes no orders.
test('E2E-20 suspended shop: not in discover, no shop page, orders refused', async ({ who }) => {
  const asha = new Customer(await who('asha'));
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  try {
    await test.step('discover does not list it; a live shop nearby is listed', async () => {
      const r = await anon<{ items: { storeId?: string; id?: string; storeSlug?: string }[] }>(ctx, 'GET', `/v1/discover/stores?lat=${QA_POINT.lat}&lng=${QA_POINT.lng}`);
      expect(r.status).toBe(200);
      const slugs = JSON.stringify(r.body);
      expect(slugs).toContain(STORES.SL.id);
      expect(slugs).not.toContain(STORES.SS.id);
    });

    await test.step('search does not return its items', async () => {
      const r = await anon(ctx, 'GET', `/v1/discover/search?q=milk&lat=${QA_POINT.lat}&lng=${QA_POINT.lng}`);
      expect(r.status).toBe(200);
      expect(JSON.stringify(r.body)).not.toContain(STORES.SS.id);
    });

    await test.step('its shop page is not found', async () => {
      const r = await anon(ctx, 'GET', `/v1/discover/stores/${STORES.SS.slug}`);
      expect(r.status).toBe(404);
    });

    await test.step('an order there is refused', async () => {
      const items = [{ storeItemId: seedItemId(STORES.SS, 1), quantity: 5 }];
      const q = await asha.api.post('/v1/me/checkout/quote', { storeId: STORES.SS.id, fulfilment: 'pickup', items, creditsToUse: 0 }, { idem: false });
      const refusedQuote = q.status >= 400 || (q.body as { orderable?: boolean }).orderable === false;
      expect(refusedQuote, `quote at a suspended shop answered ${q.status} ${q.code}`).toBe(true);
      const o = await asha.api.post('/v1/me/orders', { storeId: STORES.SS.id, fulfilment: 'pickup', items, creditsToUse: 0, expectedTotalPaise: 0 });
      expect(o.status, `order at a suspended shop answered ${o.status} ${o.code}`).toBeGreaterThanOrEqual(400);
      expect(['store_not_bookable', 'not_found']).toContain(o.code);
    });
  } finally {
    await ctx.dispose();
  }
});
