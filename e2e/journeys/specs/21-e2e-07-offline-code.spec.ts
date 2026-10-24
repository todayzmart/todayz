import { counterCode, Customer, expect, moveClock, serverNow, Shop, test, ulid, waitForCredits } from '../../fixtures/journey';
import { PEOPLE, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-07 (D-12): the POS goes offline after an online clock check; the customer reads the counter code;
// the sale is recorded with the typed code and the device time; back online within 12 h the server
// verifies the code at the recorded time step → the sale is linked and the cashback posted. Variants:
// sync after 12 h, a device clock off by more than 2 min, or a reused code → the sale is kept but not
// linked, with no ledger entries. The counter code is computed from the customer's seed (what the
// customer app shows), used once in the sync body, and never printed.

const PRICE = SL_ITEMS.qa100.pricePaise;
const QTY = 3;
const CASHBACK = Math.floor((PRICE * QTY * 500) / 1_000_000); // 5% of ₹300 at Sri Lakshmi

interface SyncResult {
  status: string;
  linkStatus: string | null;
  saleId: string | null;
  reason: unknown;
}

let seq = Math.floor(Date.now() / 1000);

async function syncSale(cashier: Shop, at: number, code: string, clockOffsetMs: number): Promise<SyncResult> {
  const iso = new Date(at).toISOString();
  const r = await cashier.api.ok<{ results: SyncResult[] }>('the POS syncs its offline sale', 'POST', `${cashier.base}/sync`, {
    cursor: 0,
    entries: [
      {
        clientId: ulid(at),
        deviceSeq: ++seq,
        kind: 'sale',
        at: iso,
        sale: {
          lines: [{ clientId: ulid(at), storeItemId: SL_ITEMS.qa100.id, name: SL_ITEMS.qa100.name, unitPricePaise: PRICE, quantity: QTY }],
          paymentMethod: 'cash',
          link: { method: 'counter_code', phone: `+91${PEOPLE.walkIn.national}`, code, codeEnteredAt: iso, clockOffsetMs },
        },
      },
    ],
  });
  expect(r.results).toHaveLength(1);
  return r.results[0] as SyncResult;
}

test.describe.configure({ mode: 'serial' });

let linkedAt = 0;
let linkedCode = '';
let credits = 0;

test('E2E-07 offline counter code → synced within 12 h → linked, cashback posted', async ({ who }) => {
  const c = new Customer(await who('walkIn'));
  const cashier = new Shop(await who('cashier'), STORES.SL);
  credits = (await c.wallet()).availableCredits;

  linkedAt = await serverNow();
  linkedCode = await counterCode(c, linkedAt);
  await moveClock({ advance: '2h' }, 0);
  const res = await syncSale(cashier, linkedAt, linkedCode, 1500);
  expect(res.status).toBe('applied');
  expect(res.linkStatus).toBe('linked');
  credits += CASHBACK;
  await waitForCredits(c, credits);
});

test('E2E-07 variant: the same code used again → sale kept, link failed, no credits', async ({ who }) => {
  test.skip(!linkedCode, 'the linked sale did not run');
  const c = new Customer(await who('walkIn'));
  const cashier = new Shop(await who('cashier'), STORES.SL);
  const res = await syncSale(cashier, linkedAt, linkedCode, 1500);
  expect(res.status).toBe('applied');
  expect(res.linkStatus).toBe('failed');
  await new Promise((done) => setTimeout(done, 3000));
  expect((await c.wallet()).availableCredits).toBe(credits);
});

test('E2E-07 variant: device clock off by more than 2 min at its last check → link failed, no credits', async ({ who }) => {
  const c = new Customer(await who('walkIn'));
  const cashier = new Shop(await who('cashier'), STORES.SL);
  const at = await serverNow();
  const code = await counterCode(c, at);
  const res = await syncSale(cashier, at, code, 3 * 60_000);
  expect(res.status).toBe('applied');
  expect(res.linkStatus).toBe('failed');
  await new Promise((done) => setTimeout(done, 3000));
  expect((await c.wallet()).availableCredits).toBe(credits);
});

test('E2E-07 variant: synced after 12 h → link failed, no credits', async ({ who }) => {
  const c = new Customer(await who('walkIn'));
  const cashier = new Shop(await who('cashier'), STORES.SL);
  const at = await serverNow();
  const code = await counterCode(c, at);
  await moveClock({ advance: '12h1m' }, 0);
  const res = await syncSale(cashier, at, code, 1500);
  expect(res.status).toBe('applied');
  expect(res.linkStatus).toBe('failed');
  await new Promise((done) => setTimeout(done, 3000));
  expect((await c.wallet()).availableCredits).toBe(credits);
});

test.fixme('E2E-07 the shop sees "Customer link failed" on the sale in the business app', async () => {
  // Deferred: the POS sales list with the link status is not in the business app at this gate.
});
