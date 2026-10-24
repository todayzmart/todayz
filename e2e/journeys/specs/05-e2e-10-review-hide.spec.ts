import { request as playwrightRequest } from '@playwright/test';

import { anon } from '../../fixtures/api';
import { Customer, customerFirstRun, expect, openApp, placeAndCollect, Shop, test, URLS } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-10: an admin hides a review → the shop's rating changes for customers. At the W2 gate the admin
// step is the admin API (D-83); the W3 gate runs it again through A-12.

async function rating(): Promise<{ count: number; avg: number | null }> {
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  try {
    const r = await anon<{ rating: { avg: number | null; count: number } }>(ctx, 'GET', `/v1/discover/stores/${STORES.SL.slug}/reviews`);
    expect(r.status, 'the public reviews of Sri Lakshmi').toBe(200);
    return r.body.rating;
  } finally {
    await ctx.dispose();
  }
}

test.describe.configure({ mode: 'serial' });

let reviewId = '';
let countWith = 0;

test('E2E-10 admin hides a review → the public rating drops; restore brings it back (admin API)', async ({ who }) => {
  const asha = new Customer(await who('asha'));
  const support = await who('support');
  const shop = new Shop(await who('owner'), STORES.SL);

  const start = await rating();
  const order = await placeAndCollect(asha, shop, [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }]);

  await test.step('the customer reviews the collected order', async () => {
    const r = await asha.api.ok<{ id: string }>('customer reviews the order', 'POST', '/v1/me/reviews', { orderId: order.id, stars: 1, text: 'E2E-10 journey review' });
    reviewId = r.id;
    await expect.poll(async () => (await rating()).count).toBe(start.count + 1);
    countWith = start.count + 1;
  });

  await test.step('support finds the review and hides it', async () => {
    const list = await support.api.ok<{ items: { id: string }[] }>('admin lists the shop’s reviews', 'GET', '/v1/admin/reviews', undefined, { params: { storeId: STORES.SL.id } });
    expect(list.items.map((i) => i.id)).toContain(reviewId);
    await support.api.ok('admin hides the review', 'POST', `/v1/admin/reviews/${reviewId}/hide`, { reason: 'E2E-10 gate: hide check' }, { idem: false });
    await expect.poll(async () => (await rating()).count).toBe(countWith - 1);
  });

  await test.step('restoring it counts it again', async () => {
    await support.api.ok('admin restores the review', 'POST', `/v1/admin/reviews/${reviewId}/restore`, { reason: 'E2E-10 gate: restore check' }, { idem: false });
    await expect.poll(async () => (await rating()).count).toBe(countWith);
  });

  await test.step('a customer cannot hide a review', async () => {
    const r = await asha.api.post(`/v1/admin/reviews/${reviewId}/hide`, { reason: 'not allowed' }, { idem: false });
    expect([401, 403]).toContain(r.status);
  });
});

test('E2E-10 the customer shop page (C-07) shows the rating', async ({ who, browser }) => {
  const asha = await who('asha');
  const app = await openApp(browser, asha, URLS.customer);
  try {
    await customerFirstRun(app.page);
    const page = app.page;
    const store = page.waitForResponse((r) => r.url().includes(`/v1/discover/stores/${STORES.SL.slug}?`) || r.url().endsWith(`/v1/discover/stores/${STORES.SL.slug}`));
    await page.goto(`/s/${STORES.SL.slug}`);
    expect((await store).status(), 'GET /v1/discover/stores/{slug} for the shop page').toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: STORES.SL.name })).toBeVisible();
    // The meta line shows the average and the count, e.g. "3.5 (1)".
    const { count } = await rating();
    await expect(page.getByText(new RegExp(`\\d(\\.\\d)? \\(${count}\\)`)).first()).toBeVisible();
  } finally {
    await app.close();
  }
});
