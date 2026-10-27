import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { overview } from '../src/test/insightsData';

// B-05 Sell on the production-shaped build with a faked API (no stack): the counter, the offline queue
// and the receipt. `TZ_QA_DIR=docs/qa/W2-11 pnpm exec playwright test sell.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const NOW = () => new Date().toISOString();
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;

function item(n: number, name: string, pricePaise: number, available: number, barcodes: string[] = [], reserved = 0) {
  const id = `01J9ZIIIIIIIIIIIIIIIIIII${String(n).padStart(2, '0')}`;
  return {
    id,
    storeId: SID,
    productId: `01J9ZPPPPPPPPPPPPPPPPPPP${String(n).padStart(2, '0')}`,
    name,
    productName: name,
    localName: null,
    brand: null,
    variant: null,
    packSize: 1,
    unit: 'pc',
    categoryId: '01J9ZCCCCCCCCCCCCCCCCCCCCC',
    barcodes,
    pricePaise,
    mrpPaise: null,
    productMrpPaise: null,
    onHand: available + reserved,
    reserved,
    available,
    lowStockAt: null,
    lowStock: false,
    isListed: true,
    stockUpdatedAt: NOW(),
    stale: false,
    createdAt: NOW(),
    updatedAt: NOW(),
  };
}

const ITEMS = [
  item(1, 'Basmati rice 1 kg', 12000, 24, ['8901234567890']),
  item(2, 'Toor dal 1 kg', 16500, 12),
  item(3, 'Sunflower oil 1 L', 14500, 9),
  item(4, 'Sugar 1 kg', 4800, 30),
  item(5, 'Tea 250 g', 11000, 6),
  item(6, 'Milk 500 ml', 3200, 40),
  item(7, 'Parle-G biscuits', 1000, 80),
  // Three of Atta's five are blocked for accepted online orders; Ghee has none left (D-270).
  item(8, 'Atta 5 kg', 26000, 2, [], 3),
  item(9, 'Ghee 500 ml', 32000, 0),
];

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
  };
};

const ok = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', headers: cors(route), body: JSON.stringify(body) });

interface WireLine {
  clientId: string;
  storeItemId?: string;
  name: string;
  unitPricePaise: number;
  quantity: number;
  manualDiscountPaise?: number;
}

function posSale(body: { clientId: string; lines: WireLine[]; billDiscountPaise?: number; paymentMethod: string }) {
  const lines = body.lines.map((l) => ({
    id: l.clientId,
    clientId: l.clientId,
    storeItemId: l.storeItemId ?? null,
    name: l.name,
    unitPricePaise: l.unitPricePaise,
    quantity: l.quantity,
    offerDiscountPaise: 0,
    manualDiscountPaise: l.manualDiscountPaise ?? 0,
    lineTotalPaise: l.unitPricePaise * l.quantity - (l.manualDiscountPaise ?? 0),
  }));
  const subtotal = lines.reduce((n, l) => n + l.lineTotalPaise, 0);
  const total = subtotal - (body.billDiscountPaise ?? 0);
  return {
    id: `sale-${body.clientId}`,
    clientId: body.clientId,
    lines,
    itemSubtotalPaise: subtotal,
    billOfferDiscountPaise: 0,
    subtotalPaise: subtotal,
    discountPaise: body.billDiscountPaise ?? 0,
    roundOffPaise: 0,
    creditsUsed: 0,
    cashbackCredits: 0,
    totalPaise: total,
    paymentMethod: body.paymentMethod,
    link: null,
    status: 'completed',
    reversedAt: null,
    reversalReason: null,
    soldAt: NOW(),
    receivedAt: NOW(),
    receiptToken: 'EXAMPLE_receipt_000000',
    version: 1,
    serverNow: NOW(),
  };
}

// The store's sales from the server (W2-16): one of each link state, three offline-code failures.
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();
function storeSale(n: number, min: number, totalPaise: number, units: number, paymentMethod: string, offline: boolean, link: unknown, status = 'completed') {
  const id = `01J9ZQQQQQQQQQQQQQQQQQQQ${String(n).padStart(2, '0')}`;
  const cashier = n % 2 ? { id: '01J9ZUUUUUUUUUUUUUUUUUUU01', name: 'Ravi' } : { id: '01J9ZUUUUUUUUUUUUUUUUUUU02', name: 'Meena' };
  return { id, clientId: id, soldAt: ago(min), receivedAt: ago(offline ? min - 3 : min), totalPaise, lineCount: units, unitCount: units, paymentMethod, creditsUsed: n === 1 ? 20 : 0, cashbackCredits: 0, status, reversedAt: status === 'reversed' ? ago(min - 1) : null, offline, link, cashier };
}

// Counter sales (D-268): the strip's totals and one bill in full, read from the same rows.
function salesSummary() {
  const done = STORE_SALES.filter((s) => s.status === 'completed');
  const total = done.reduce((n, s) => n + s.totalPaise, 0);
  const by = (m: string) => done.filter((s) => s.paymentMethod === m);
  return {
    from: null, to: null, salesPaise: total, billCount: done.length, averageBillPaise: Math.floor(total / done.length),
    unitCount: done.reduce((n, s) => n + s.unitCount, 0), reversedPaise: 1000, reversedCount: 1, creditsUsed: 20, cashbackCredits: 0,
    offlineCount: done.filter((s) => s.offline).length, linkedCount: 1,
    byPayment: ['cash', 'upi', 'card'].map((m) => ({ method: m, billCount: by(m).length, totalPaise: by(m).reduce((n, s) => n + s.totalPaise, 0) })),
    byCashier: [
      { cashier: { id: '01J9ZUUUUUUUUUUUUUUUUUUU01', name: 'Ravi' }, billCount: 3, totalPaise: 0 },
      { cashier: { id: '01J9ZUUUUUUUUUUUUUUUUUUU02', name: 'Meena' }, billCount: 2, totalPaise: 0 },
    ],
    serverNow: NOW(),
  };
}

function saleDetail(id: string) {
  const s = STORE_SALES.find((x) => x.id === id);
  if (!s) return null;
  const line = (n: number, name: string, unit: number, qty: number, off = 0) => ({
    id: `${id.slice(0, 24)}L${n}`, clientId: `${id.slice(0, 24)}L${n}`, storeItemId: ITEMS[n]?.id ?? null, name, unitPricePaise: unit, quantity: qty,
    offerDiscountPaise: off, manualDiscountPaise: 0, lineTotalPaise: unit * qty - off,
  });
  const lines = s.totalPaise === 38200 ? [line(0, 'Basmati rice 1 kg', 12000, 2), line(4, 'Tea 250 g', 11000, 1), line(5, 'Milk 500 ml', 3200, 1)] : [line(1, 'Toor dal 1 kg', s.totalPaise / s.unitCount, s.unitCount)];
  const subtotal = lines.reduce((n, l) => n + l.lineTotalPaise, 0);
  return {
    ...s, lines, itemSubtotalPaise: subtotal, billOfferDiscountPaise: 0, subtotalPaise: subtotal, discountPaise: subtotal - s.totalPaise, roundOffPaise: 0,
    deviceId: '01J9ZDDDDDDDDDDDDDDDDDD7K2', reversalReason: s.status === 'reversed' ? 'Wrong item billed' : null,
    reversedBy: s.status === 'reversed' ? { id: '01J9ZUUUUUUUUUUUUUUUUUUU09', name: 'Asha' } : null,
    reversibleUntil: s.status === 'completed' ? new Date(Date.now() + 20 * 3_600_000).toISOString() : null, version: 1, serverNow: NOW(),
  };
}
const failed = (failure: string) => ({ method: 'counter_code', status: 'failed', customerName: null, failure });
const STORE_SALES = [
  storeSale(1, 4, 38200, 4, 'upi', false, { method: 'bill_qr', status: 'linked', customerName: 'Ananya', failure: null }),
  storeSale(2, 18, 9600, 2, 'cash', true, failed('code_used')),
  storeSale(3, 35, 16500, 1, 'cash', true, failed('clock_off')),
  storeSale(4, 52, 26000, 1, 'card', true, failed('too_late')),
  storeSale(5, 70, 4800, 1, 'cash', true, { method: 'counter_code', status: 'pending_verification', customerName: null, failure: null }),
  storeSale(6, 95, 1000, 1, 'cash', false, null, 'reversed'),
];

async function fakeApi(page: Page) {
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role: 'owner', stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (url.pathname === `${base}/sync`) {
      const body = req.postDataJSON() as { cursor: number; entries: { clientId: string; deviceSeq: number }[] };
      return ok(route, {
        results: body.entries.map((e) => ({ clientId: e.clientId, deviceSeq: e.deviceSeq, status: 'applied', reason: null, saleId: `sale-${e.clientId}`, movementId: null, linkStatus: null, receiptToken: 'EXAMPLE_receipt_000000' })),
        changes: [],
        cursor: 1,
        hasMore: false,
        serverNow: NOW(),
      });
    }
    if (url.pathname === `${base}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: NOW() });
    if (url.pathname === `${base}/stream`) return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (url.pathname === `${base}/asks`) return ok(route, { asks: [], openCount: 0, nextCursor: null, serverNow: NOW() });
    // The owner's sidebar reads today's overview for the Stock badge and the setup card (W3-03).
    if (url.pathname === `${base}/overview`) return ok(route, overview({ newOrders: [] }));
    if (url.pathname === `${base}/items`) return ok(route, { items: ITEMS, nextCursor: null });
    if (url.pathname === `${base}/pos/sales` && req.method() === 'GET') return ok(route, { items: STORE_SALES, nextCursor: null, serverNow: NOW() });
    if (url.pathname === `${base}/pos/sales-summary`) return ok(route, salesSummary());
    if (url.pathname.startsWith(`${base}/pos/sales/`) && req.method() === 'GET') {
      const detail = saleDetail(url.pathname.split('/').pop() ?? '');
      if (detail) return ok(route, detail);
    }
    if (url.pathname === `${base}/pos/sales`) return ok(route, posSale(req.postDataJSON() as Parameters<typeof posSale>[0]), 201);
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }) });
  });
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
  await fakeApi(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const tileOf = (page: Page, name: string) => page.getByRole('region', { name: 'Items' }).getByRole('button', { name: new RegExp(name) });

test('desktop: counter with a bill, then charge and the receipt (B-05)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/sell');
  await expect(page.getByRole('heading', { level: 1, name: 'Sell' })).toBeVisible();
  await expect(tileOf(page, 'Basmati rice')).toBeVisible();
  await tileOf(page, 'Basmati rice').click();
  await tileOf(page, 'Basmati rice').click();
  await tileOf(page, 'Tea 250 g').click();
  await tileOf(page, 'Milk 500 ml').click();
  await expect(page.locator('.tz-amt')).toHaveText('₹382');
  await shot(page, 'sell-counter-desktop.png');
  await page.getByRole('button', { name: 'UPI' }).click();
  await page.getByRole('button', { name: 'Charge' }).click();
  await expect(page.getByText('Sale of ₹382 done')).toBeVisible();
  await page.getByRole('button', { name: 'Receipt' }).click();
  const receipt = page.getByRole('dialog', { name: 'Receipt' });
  await expect(receipt).toBeVisible();
  await expect(receipt.getByText('Asha Stores, Indiranagar')).toBeVisible();
  await shot(page, 'sell-receipt.png');
});

test('desktop: offline sales queue on the device and show as waiting (B-05)', async ({ page, context }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/sell');
  await expect(tileOf(page, 'Sugar 1 kg')).toBeVisible();
  await context.setOffline(true);
  await expect(page.getByText(/^Offline/)).toBeVisible();
  for (const name of ['Sugar 1 kg', 'Parle-G biscuits']) {
    await tileOf(page, name).click();
    await page.getByRole('button', { name: 'Charge' }).click();
    await expect(page.getByText(/saved on this device/)).toBeVisible();
  }
  await expect(page.getByText(/Offline · 2 sales waiting to sync/)).toBeVisible();
  await page.getByRole('button', { name: /^Sales/ }).click();
  const sales = page.getByRole('dialog', { name: 'Sales on this device' });
  await expect(sales.getByText('Waiting to sync')).toHaveCount(2);
  await shot(page, 'sell-offline-queue.png');
});

test('desktop: a line never goes past available; the count dialog with and without blocked units (D-270)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/sell');
  const atta = tileOf(page, 'Atta 5 kg');
  await expect(atta).toContainText('3 blocked for online orders');
  await tileOf(page, 'Basmati rice').click();
  await atta.click();
  await atta.click();
  await shot(page, 'pos-stock-tile-blocked.png');
  await atta.click();
  const dialog = page.getByRole('dialog', { name: 'Update stock' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('checkbox', { name: 'This count includes the 3 units set aside for online orders' })).not.toBeChecked();
  await dialog.getByLabel('Counted now').fill('4');
  await expect(dialog.getByText('On hand will be 7, available 4')).toBeVisible();
  await shot(page, 'pos-stock-dialog-blocked.png');
  await dialog.getByRole('button', { name: 'Save count' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.locator('.tz-amt')).toHaveText('₹900');
  await expect(atta).toContainText('4 in stock');

  const search = page.getByRole('searchbox', { name: 'Search or scan an item' }).or(page.getByRole('textbox', { name: 'Search or scan an item' }));
  await search.fill('Ghee');
  await page.getByRole('region', { name: 'Items' }).getByRole('button', { name: /Ghee 500 ml/ }).click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('checkbox')).toHaveCount(0);
  await dialog.getByLabel('Counted now').fill('6');
  await shot(page, 'pos-stock-dialog-plain.png');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.locator('.tz-amt')).toHaveText('₹900');
});

test('phone: counter with the bill bar and the bill sheet (B-05)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/sell');
  await tileOf(page, 'Toor dal').click();
  await tileOf(page, 'Sugar 1 kg').click();
  await expect(page.getByRole('button', { name: /View bill/ })).toBeVisible();
  await shot(page, 'sell-counter-phone.png');
  await page.getByRole('button', { name: /View bill/ }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await shot(page, 'sell-bill-sheet-phone.png');
});

test('phone 360: counter sales with each link status and why a link failed (B-04, D-268)', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/sell');
  await page.getByRole('link', { name: 'View sales' }).click();
  await expect(page).toHaveURL(/\/orders\?view=sales/);
  const list = page.getByRole('list', { name: 'Counter sales' });
  await expect(list.getByText('Customer link failed')).toHaveCount(3);
  await expect(list.getByText('The counter code was already used.')).toBeVisible();
  await expect(list.getByText('The device clock was off.')).toBeVisible();
  await expect(list.getByText('The sale synced too late to check the code.')).toBeVisible();
  await expect(list.getByText('Linked to Ananya')).toBeVisible();
  await expect(list.getByText('Link pending')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Counter sales at a glance' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  await shot(page, 'counter-sales-360.png');
  await list.getByRole('button', { name: /Open bill .*QQ02/ }).click();
  const detail = page.getByRole('dialog', { name: /^Bill / });
  await expect(detail.getByText('The counter code was already used.')).toBeVisible();
  await expect(detail.getByRole('button', { name: 'Reverse' })).toBeVisible();
  await shot(page, 'counter-sales-detail-360.png');
});

test('desktop: counter sales table, totals and the bill panel (B-04, D-268)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/orders');
  await page.getByRole('radio', { name: 'Counter sales' }).click();
  await expect(page).toHaveURL(/view=sales/);
  const table = page.getByRole('table', { name: 'Counter sales' });
  await expect(table.getByText('Linked to Ananya')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Counter sales at a glance' }).getByText('Sales', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Export CSV' })).toBeVisible();
  await shot(page, 'counter-sales-desktop.png');
  await table.getByRole('button', { name: /Open bill .*QQ01/ }).click();
  const detail = page.getByRole('dialog', { name: /^Bill / });
  await expect(detail.getByText('Basmati rice 1 kg')).toBeVisible();
  await expect(detail.getByText('Ravi')).toBeVisible();
  await expect(page).toHaveURL(/sale=01J9ZQQ/);
  await shot(page, 'counter-sales-detail-desktop.png');
});
