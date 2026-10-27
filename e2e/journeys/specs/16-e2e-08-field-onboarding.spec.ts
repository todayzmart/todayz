import { Customer, expect, placeAndCollect, Shop, test } from '../../fixtures/journey';
import { smsCodeSince } from '../../fixtures/principals';
import { ALL_DAY, QA_POINT } from '../../fixtures/seed';

// E2E-08: a field agent onboards a shop (business, store, first item) → the owner confirms with the
// code sent to their phone, and the business and store go live at once (D-274: nobody approves first)
// → the shop is found in search → the first order is booked, accepted and collected → the field agent
// marks the shop checked from the daily queue (A-24), which releases its payouts. Admin steps use the
// admin API (D-83); the owner's confirm code is read from the mock SMS and never printed.
interface Created {
  id: string;
  stores?: { id: string }[];
}
interface Category {
  id: string;
}
interface Search {
  results: { store: { storeId: string }; items: { storeItemId: string }[] }[];
}

test('E2E-08 field agent onboards a shop → owner confirms → live at once in search → first order → agent checks it', async ({ who }) => {
  const agent = await who('fieldAgent');
  const admin = await who('superAdmin');
  const customer = new Customer(await who('walkIn'));
  const ownerPhone = '9000000048';
  const tag = `${Date.now()}`.slice(-6);
  const brandName = `E2E Onboard ${tag}`;

  const category = await test.step('the agent picks a category', async () => {
    const list = await agent.api.ok<{ items: Category[] }>('agent lists categories', 'GET', '/v1/discover/categories');
    expect(list.items.length).toBeGreaterThan(0);
    return list.items[0]?.id ?? '';
  });

  const bid = await test.step('the agent creates the business for the owner’s phone', async () => {
    const b = await agent.api.ok<Created>('agent creates the business', 'POST', '/v1/admin/field/businesses', {
      brandName,
      ownerPhone: `+91${ownerPhone}`,
      categoryId: category,
    });
    return b.id;
  });

  const sid = await test.step('the agent adds the store: address, pin, phone, all-day hours', async () => {
    const s = await agent.api.ok<{ id: string }>('agent adds the store', 'POST', `/v1/admin/field/businesses/${bid}/stores`, {
      name: `${brandName} Store`,
      addressLine1: '5, 100 Feet Road',
      locality: 'Indiranagar',
      city: 'Bengaluru',
      pincode: '560038',
      location: { lat: QA_POINT.lat + 0.0004, lng: QA_POINT.lng + 0.0004 },
      phone: '+919000000049',
      // Weekly hours only: holidays are the owner's to add later (FieldStoreHours).
      hours: { weekly: ALL_DAY.weekly },
    });
    return s.id;
  });

  const item = await test.step('the agent lists a product with a price and opening stock', async () => {
    const found = await agent.api.ok<{ items: { id: string; name: string }[] }>('agent searches the catalogue', 'GET', `/v1/admin/field/businesses/${bid}/stores/${sid}/products`, undefined, { params: { q: 'atta' } });
    expect(found.items.length, 'the catalogue has an atta').toBeGreaterThan(0);
    const created = await agent.api.ok<{ id: string }>('agent lists the item', 'POST', `/v1/admin/field/businesses/${bid}/stores/${sid}/items`, {
      productId: found.items[0]?.id,
      pricePaise: 20000,
      openingStock: 20,
    });
    return created.id;
  });

  await test.step('the owner confirms with the code sent to their phone', async () => {
    const since = new Date(Date.now() - 1000).toISOString();
    await agent.api.ok('agent sends the owner the code', 'POST', `/v1/admin/field/businesses/${bid}/owner-confirm`, {});
    let code = '';
    await expect
      .poll(async () => {
        try {
          code = await smsCodeSince(ownerPhone, since);
        } catch {
          code = '';
        }
        return code.length;
      }, { message: 'the mock SMS holds the owner code', timeout: 20_000 })
      .toBe(6);
    await agent.api.ok('owner gives the code to the agent', 'POST', `/v1/admin/field/businesses/${bid}/owner-confirm/verify`, { code });
  });

  // The owner signs in only after the confirm: a sign-in code to the number inside the resend gap
  // would make the agent's owner-confirm send 429 (01 §2 counts codes per number).
  const owner = await who('newOwner');
  await test.step('the confirm put the business and its store live, unchecked', async () => {
    // D-241, D-274: the owner's code takes the business and its complete stores live, so the owner
    // need not open the app, and no admin approves anything first.
    const store = await owner.api.ok<{ status: string }>('owner reads the store', 'GET', `/v1/biz/businesses/${bid}/stores/${sid}`);
    expect(store.status).toBe('live');
    const biz = await admin.api.ok<{ status: string; checkStatus: string }>('admin reads the business', 'GET', `/v1/admin/businesses/${bid}`);
    expect(biz.status).toBe('verified');
    expect(biz.checkStatus).toBe('unchecked');
  });

  const store = { id: sid, slug: '', name: `${brandName} Store`, no: 0 };
  const placed = await test.step('the new shop shows in search near the QA point', async () => {
    let storeItemId = '';
    await expect
      .poll(async () => {
        const found = await customer.api.ok<Search>('customer searches', 'GET', '/v1/discover/search', undefined, { params: { q: 'atta', lat: QA_POINT.lat, lng: QA_POINT.lng } });
        const mine = found.results.find((r) => r.store.storeId === sid);
        storeItemId = mine?.items[0]?.storeItemId ?? '';
        return storeItemId;
      }, { message: 'the new store is in the results', timeout: 30_000 })
      .not.toBe('');
    return storeItemId;
  });

  await test.step('the first order is booked, accepted and collected by the new owner', async () => {
    expect(item).toBeTruthy();
    const shop = new Shop(owner, store, bid);
    await placeAndCollect(customer, shop, [{ storeItemId: placed, quantity: 1 }]);
  });

  await test.step('the field agent finds the shop on the check queue and marks it checked', async () => {
    const queue = await agent.api.ok<{ items: { id: string }[] }>('agent reads the shop check queue', 'GET', '/v1/admin/shop-checks', undefined, { params: { limit: 100 } });
    expect(queue.items.map((i) => i.id)).toContain(bid);
    const checked = await agent.api.ok<{ checkStatus: string; status: string }>('agent marks the shop checked', 'POST', `/v1/admin/businesses/${bid}/check`, { note: 'E2E-08 visit: stock and photos match' });
    expect(checked.checkStatus).toBe('checked');
    expect(checked.status).toBe('verified');
  });
});
