import { Principal, trySignIn } from '../../fixtures/principals';
import { Customer, expect, test, URLS } from '../../fixtures/journey';
import { PEOPLE, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-19 (builder-written): an admin blocks a customer → the customer's sessions end, sign-in and
// ordering are refused, an open order is left for the admin cancel, and unblocking restores them.
// Admin steps go through the admin API (D-83). Three OTP sends to one phone (of 5 an hour).

interface Found {
  items: { id: string; status: string }[];
}
interface AdminCustomer {
  status: string;
  sessions: { id: string }[];
}
interface AuditList {
  items: { action: string; actorId?: string | null; after?: Record<string, unknown> | null }[];
}

test('E2E-19 blocked customer cannot sign in or order; unblock restores them', async ({ who }) => {
  const c = new Customer(await who('blocked'));
  const admin = await who('superAdmin');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 1 }];
  let again: Principal | undefined;

  try {
    const open = await test.step('the customer has an open order', async () => c.place(STORES.SL, items));

    const id = await test.step('admin finds the customer by phone (A-06)', async () => {
      // The phone goes in the body, never in a URL (W3-05 review).
      const found = await admin.api.ok<Found>('admin searches the phone', 'POST', '/v1/admin/customer-search', {
        phone: PEOPLE.blocked.national,
      });
      expect(found.items, 'one customer has the number').toHaveLength(1);
      expect(found.items[0]?.id).toBe(c.who.userId);
      return c.who.userId;
    });
    const base = `/v1/admin/customers/${id}`;

    await test.step('admin blocks with a reason: every session ends', async () => {
      const r = await admin.api.call<AdminCustomer>('POST', `${base}/block`, { reason: 'E2E-19 fake orders' });
      expect(r.status, `block answered ${r.status} ${r.code}`).toBe(200);
      expect(r.body.status).toBe('blocked');
      expect(r.body.sessions).toHaveLength(0);
      const twice = await admin.api.call('POST', `${base}/block`, { reason: 'E2E-19 again' });
      expect(twice.code).toBe('invalid_transition');
    });

    await test.step('the customer is refused at once, ordering too', async () => {
      const me = await c.api.get('/v1/me');
      expect(me.status).toBe(403);
      expect(me.code).toBe('account_blocked');
      const quote = await c.api.post('/v1/me/checkout/quote', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 0 }, { idem: false });
      expect(quote.code).toBe('account_blocked');
      const order = await c.api.post('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 0, expectedTotalPaise: open.totalPaise });
      expect(order.code).toBe('account_blocked');
    });

    await test.step('the session cannot be renewed', async () => {
      const r = await c.who.request().post(`${URLS.api}/v1/auth/refresh`, {
        headers: { Origin: URLS.customer, 'X-TZ-Client': 'e2e-journeys/1.0.0' },
        data: { app: 'customer' },
      });
      expect(r.status(), 'refresh after block').toBeGreaterThanOrEqual(400);
      c.who.forget();
    });

    await test.step('a new sign-in is refused with account_blocked', async () => {
      const r = await trySignIn(PEOPLE.blocked);
      expect(r.status).toBe(403);
      expect(r.code).toBe('account_blocked');
    });

    await test.step('the open order stays open for the admin cancel', async () => {
      const list = await admin.api.ok<Found>('admin lists the customer orders', 'GET', '/v1/admin/orders', undefined, { params: { customerId: id } });
      expect(list.items.find((o) => o.id === open.id)?.status).toBe(open.status);
    });

    await test.step('the block is in the customer audit panel with the reason', async () => {
      const audit = await admin.api.ok<AuditList>('admin reads the subject audit', 'GET', '/v1/admin/audit-log/subject', undefined, {
        params: { subjectType: 'user', subjectId: id },
      });
      const block = audit.items.find((e) => e.action === 'customer.block');
      expect(block?.actorId).toBe(admin.userId);
      expect(block?.after?.['reason']).toBe('E2E-19 fake orders');
    });

    await test.step('admin unblocks; the customer signs in and orders again', async () => {
      const r = await admin.api.call<AdminCustomer>('POST', `${base}/unblock`, { reason: 'E2E-19 spoke to the customer' });
      expect(r.status, `unblock answered ${r.status} ${r.code}`).toBe(200);
      expect(r.body.status).toBe('active');
      again = await new Principal(PEOPLE.blocked).open();
      const back = new Customer(again);
      expect(again.userId, 'the same account').toBe(id);
      const me = await back.api.get('/v1/me');
      expect(me.status).toBe(200);
      const second = await back.place(STORES.SL, items);
      for (const o of [open, second]) {
        await back.api.ok('customer cancels the order', 'POST', `/v1/me/orders/${o.id}/cancel`, { reason: 'changed_mind' });
      }
    });
  } finally {
    // A run that failed half-way leaves the person unblocked (409 invalid_transition when it already is).
    await admin.api.call('POST', `/v1/admin/customers/${c.who.userId}/unblock`, { reason: 'E2E-19 clean-up' });
    await again?.dispose();
  }
});
