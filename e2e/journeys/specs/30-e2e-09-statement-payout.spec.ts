import { expect, moveClock, openApp, serverNow, test, URLS } from '../../fixtures/journey';
import { LAKSHMI } from '../../fixtures/seed';

// E2E-09 (D-09, A-11, B-10): month-end. The test clock moves past 00:30 IST on the 1st, the payouts.close
// job closes last month's statements, and Lakshmi Traders has a pending payout. A finance admin marks it
// paid with the transfer's UTR (a † step-up action); the business then reads Paid with that UTR on its
// statements and on the Credits screen. This spec moves the clock, so it runs last; global teardown puts
// the clock back. The admin steps use the admin API (D-83).
interface Payout {
  id: string;
  businessId: string;
  amountPaise: number;
  status: string;
  period: string;
  utr: string | null;
}
interface Statements {
  items: { period: string; payout: { status: string; utr: string | null } | null }[];
}

const IST_MS = 5.5 * 3_600_000;

test('E2E-09 month-end statement → finance marks the payout paid → the business sees Paid with the UTR', async ({ who, browser }) => {
  const finance = await who('finance1');
  const superAdmin = await who('superAdmin');
  const owner = await who('owner');

  const period = await test.step('the clock moves just past the 1st, 01:00 IST: last month closes', async () => {
    const ist = new Date((await serverNow()) + IST_MS);
    const firstOfNext = Date.UTC(ist.getUTCFullYear(), ist.getUTCMonth() + 1, 1, 1, 0);
    const hours = Math.ceil((firstOfNext - ist.getTime()) / 3_600_000);
    await moveClock({ advance: `${hours}h` }, 30);
    return `${ist.getUTCFullYear()}-${String(ist.getUTCMonth() + 1).padStart(2, '0')}`;
  });

  const payout = await test.step('the close job has made a pending payout for Lakshmi Traders', async () => {
    const find = async () => {
      const list = await finance.api.ok<{ items: Payout[] }>('finance lists the payouts', 'GET', '/v1/admin/payouts', undefined, { params: { period } });
      return list.items.find((p) => p.businessId === LAKSHMI.id);
    };
    let found = await find();
    if (!found) {
      await superAdmin.stepUp();
      await superAdmin.api.ok('super admin runs the payout close', 'POST', '/v1/admin/jobs/payouts_statement_close/run', { reason: 'W3-14 gate: month end', periodKey: period }, { want: [200, 202] });
      await expect.poll(async () => (await find())?.id ?? '', { message: 'the payout exists', timeout: 40_000 }).not.toBe('');
      found = await find();
    }
    expect(found?.status, 'the payout is pending').toBe('pending');
    return found as Payout;
  });

  const utr = `W314${Date.now()}`.slice(0, 16);
  await test.step('finance marks it paid with the UTR (step-up)', async () => {
    await finance.stepUp();
    const paid = await finance.api.ok<{ status: string; utr: string }>('finance marks the payout paid', 'POST', `/v1/admin/payouts/${payout.id}/mark-paid`, {
      utr,
      amountPaise: payout.amountPaise,
      reason: 'W3-14 gate: paid by UPI',
    });
    expect(paid.status).toBe('paid');
    expect(paid.utr.toUpperCase()).toBe(utr.toUpperCase());
  });

  await test.step('the business statements read Paid with the UTR', async () => {
    const list = await owner.api.ok<Statements>('owner lists the statements', 'GET', `/v1/biz/businesses/${LAKSHMI.id}/statements`);
    const row = list.items.find((s) => s.period === period);
    expect(row?.payout?.status).toBe('paid');
    expect(row?.payout?.utr?.toUpperCase()).toBe(utr.toUpperCase());
  });

  await test.step('the Credits screen in the business app shows Paid and the UTR', async () => {
    const app = await openApp(browser, owner, URLS.business);
    try {
      await app.page.goto('/credits');
      await expect(app.page.getByText('Paid').first()).toBeVisible();
      await expect(app.page.getByText(utr.toUpperCase()).first()).toBeVisible();
    } finally {
      await app.close();
    }
  });
});
