import { Customer, expect, test, waitForCredits, type Order } from '../../fixtures/journey';
import { SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-12: a super admin changes `credit_max_per_order` 30 → 40 with step-up → the next checkout allows
// 40; the audit entry is visible. The first test is the "before" half (30 used, 40 refused); the second
// changes the setting through the admin API (A-17, D-83) and puts it back.
//
// More credits than `credit_max_per_order` is a 409 `credit_max_exceeded` (04-api, 08 CR-02; the
// lead's answer to Q-1 in docs/qa/W2-13/REPORT.md; D-216, W2-02 finish).
test('E2E-12 before the change: 30 credits are used, 40 are refused with credit_max_exceeded', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const credits = (await c.wallet()).availableCredits;
  test.skip(credits < 40, 'the referee needs 40 credits for this check (E2E-02 and E2E-06 run first)');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 5 }];

  await test.step('30 credits: quoted, placed, held, and given back on cancel', async () => {
    const q = await c.quote(STORES.SL, items, 30);
    expect(q.creditsAllowed, 'checkout allows at most 30').toBe(30);
    expect(q.creditsUsed).toBe(30);
    expect(q.totalPaise).toBe(50000 - 3000);
    const placed = await c.api.post<Order>('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 30, expectedTotalPaise: q.totalPaise });
    expect(placed.status, `order with 30 credits: ${placed.code}`).toBe(201);
    expect(placed.body.creditsUsed, '30 credits are held').toBe(30);
    await c.api.ok('customer cancels the order', 'POST', `/v1/me/orders/${placed.body.id}/cancel`, { reason: 'changed_mind' });
    await waitForCredits(c, credits);
  });

  await test.step('40 credits: refused with credit_max_exceeded, nothing held', async () => {
    const r = await c.api.post<Order>('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 40, expectedTotalPaise: 50000 - 4000 });
    expect(r.status, `order with 40 credits: ${r.code}`).toBe(409);
    expect(r.code).toBe('credit_max_exceeded');
    await waitForCredits(c, credits);
  });
});

interface Setting {
  key: string;
  value: unknown;
  version: number;
}
interface Change {
  actorId: string | null;
  from: unknown;
  to: unknown;
  reason: string;
}
interface AuditList {
  items: { action: string; actorId?: string | null; before?: Record<string, unknown> | null; after?: Record<string, unknown> | null }[];
}

const KEY = 'credit_max_per_order';
const SETTING = `/v1/admin/settings/${KEY}`;

test('E2E-12 super admin sets credit_max_per_order to 40 with step-up; checkout allows 40; audit entry visible', async ({ who }) => {
  const c = new Customer(await who('referee'));
  const admin = await who('superAdmin');
  const credits = (await c.wallet()).availableCredits;
  test.skip(credits < 40, 'the referee needs 40 credits for this check (E2E-02 and E2E-06 run first)');
  const items = [{ storeItemId: SL_ITEMS.qa100.id, quantity: 5 }];

  const before = await test.step('the setting is 30 and only a super admin may change it', async () => {
    const list = await admin.api.ok<{ items: (Setting & { money: boolean; editable: boolean })[] }>('admin lists the settings', 'GET', '/v1/admin/settings');
    const row = list.items.find((i) => i.key === KEY);
    expect(row?.value, 'the seeded cap').toBe(30);
    expect(row?.money, 'a money setting').toBe(true);
    expect(row?.editable, 'editable by a super admin').toBe(true);
    return row?.value;
  });

  try {
    await test.step('with a fresh authenticator code, the super admin sets it to 40 with a reason', async () => {
      await admin.stepUp();
      const r = await admin.api.call<{ setting: Setting }>('PATCH', SETTING, { value: 40, reason: 'E2E-12 festival week' });
      expect(r.status, `PATCH answered ${r.status} ${r.code}`).toBe(200);
      expect(r.body.setting.value).toBe(40);
    });

    await test.step('the next checkout allows 40 credits; the order holds them and gives them back on cancel', async () => {
      // Servers pick the change up on NOTIFY, or within the 30 s cache TTL if one is missed.
      await expect
        .poll(async () => (await c.quote(STORES.SL, items, 40)).creditsAllowed, { message: 'checkout allows 40', timeout: 35_000, intervals: [500, 1_000, 2_000] })
        .toBe(40);
      const q = await c.quote(STORES.SL, items, 40);
      expect(q.creditsUsed).toBe(40);
      expect(q.totalPaise).toBe(50000 - 4000);
      const placed = await c.api.post<Order>('/v1/me/orders', { storeId: STORES.SL.id, fulfilment: 'pickup', items, creditsToUse: 40, expectedTotalPaise: q.totalPaise });
      expect(placed.status, `order with 40 credits: ${placed.code}`).toBe(201);
      expect(placed.body.creditsUsed, '40 credits are held').toBe(40);
      await c.api.ok('customer cancels the order', 'POST', `/v1/me/orders/${placed.body.id}/cancel`, { reason: 'changed_mind' });
      await waitForCredits(c, credits);
    });

    await test.step('the change is in the setting history and the audit log, with who and why', async () => {
      const history = await admin.api.ok<{ items: Change[] }>('admin reads the setting history', 'GET', `${SETTING}/history`);
      const last = history.items[0];
      expect(last?.actorId).toBe(admin.userId);
      expect([last?.from, last?.to]).toEqual([30, 40]);
      expect(last?.reason).toBe('E2E-12 festival week');
      const audit = await admin.api.ok<AuditList>('admin reads the subject audit', 'GET', '/v1/admin/audit-log/subject', undefined, {
        params: { subjectType: 'platform_setting', subjectId: KEY },
      });
      const entry = audit.items.find((e) => e.action === 'settings.update' && e.after?.['value'] === 40);
      expect(entry?.actorId).toBe(admin.userId);
      expect(entry?.before?.['value']).toBe(30);
    });
  } finally {
    // Back to the seeded cap, so the gate can run again without a reset. The step-up is good for 10 minutes.
    if (before !== undefined) {
      const r = await admin.api.call('PATCH', SETTING, { value: before, reason: 'E2E-12 clean-up' });
      expect(r.status, `restore answered ${r.status} ${r.code}`).toBe(200);
    }
  }

  await test.step('back at 30, 40 credits are refused again', async () => {
    await expect
      .poll(async () => (await c.quote(STORES.SL, items, 40)).creditsAllowed, { message: 'checkout allows 30 again', timeout: 35_000, intervals: [500, 1_000, 2_000] })
      .toBe(30);
  });
});
