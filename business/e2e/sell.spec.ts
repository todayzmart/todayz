import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-05 Sell on the production-shaped build with a faked API (no stack): the counter, the offline queue
// and the receipt. `TZ_QA_DIR=docs/qa/W2-11 pnpm exec playwright test sell.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const NOW = () => new Date().toISOString();
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;

function item(n: number, name: string, pricePaise: number, available: number, barcodes: string[] = []) {
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
    onHand: available,
    reserved: 0,
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
  item(8, 'Atta 5 kg', 26000, 2),
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
    if (url.pathname === `${base}/items`) return ok(route, { items: ITEMS, nextCursor: null });
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
