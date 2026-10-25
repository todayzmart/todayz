import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-04 delivery (W2-02 delivery, BK-24–27) at 360 px on the production-shaped build with a faked API (no stack).
// `TZ_QA_DIR=docs/qa/W2-02-delivery pnpm exec playwright test orders.delivery.shots.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const OUT = '01J9ZOOOOOOOOOOOOOOOOOOOO1';
const FAILED = '01J9ZOOOOOOOOOOOOOOOOOOOO2';
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;
const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();
const PHONE = '+919000000001'; // a fake number

const summary = (id: string, number: string, over: Record<string, unknown>) => ({
  id,
  number,
  customerName: 'Meera',
  fulfilment: 'delivery',
  status: 'out_for_delivery',
  statusReason: null,
  itemCount: 3,
  totalPaise: 54600,
  acceptBy: at(-40),
  holdUntil: null,
  dispatchBy: null,
  deliveryFailedAt: null,
  returnOutcome: null,
  customerPhone: null,
  placedAt: at(-45),
  seen: true,
  version: 3,
  ...over,
});

const outRow = () => summary(OUT, 'TZ-1042', { customerPhone: PHONE });
const failedRow = () => summary(FAILED, 'TZ-1043', { status: 'delivery_failed', statusReason: 'customer_unreachable', deliveryFailedAt: at(-125) });

const line = (n: number, name: string, packSize: number, unit: string, unitPricePaise: number, quantityRequested: number) => ({
  id: `01J9ZLLLLLLLLLLLLLLLLLLLL${n}`,
  storeItemId: `01J9ZIIIIIIIIIIIIIIIIIIII${n}`,
  name,
  variant: null,
  packSize,
  unit,
  imageKey: null,
  unitPricePaise,
  quantityRequested,
  quantityAccepted: quantityRequested,
  lineTotalPaise: unitPricePaise * quantityRequested,
  priceSource: 'base',
  available: 9,
});

const detail = (row: ReturnType<typeof summary>, phone: string | null) => ({
  ...row,
  customer: { name: 'Meera', phoneMasked: '+91 90xxx xx001', phone },
  deliveryAddress: { label: 'Home', line1: '12, 3rd Cross', line2: 'HAL 2nd Stage', landmark: 'Opp. Ganesha temple', pincode: '560038', lat: 12.97, lng: 77.64 },
  statusNote: null,
  lines: [line(1, 'Amul Butter', 100, 'g', 5800, 2), line(2, 'Toor dal', 1, 'kg', 12900, 2), line(3, 'Sunflower oil', 1, 'l', 14500, 1)],
  subtotalPaise: 52100,
  discountPaise: 0,
  itemSubtotalPaise: 52100,
  itemsAfterOffersPaise: 52100,
  creditsUsed: 0,
  deliveryFeePaise: 2500,
  paymentStatus: 'pay_at_store',
  paymentMethod: null,
  codeLocked: false,
  codeAttemptsLeft: 5,
  partialConfirmBy: null,
  extendedAt: null,
  acceptedAt: at(-40),
  collectedAt: null,
  returnConfirmedAt: null,
  timeline: [
    { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: at(-45) },
    { fromStatus: 'placed', toStatus: 'accepted', actor: 'store', reason: null, at: at(-40) },
    { fromStatus: 'accepted', toStatus: 'out_for_delivery', actor: 'store', reason: null, at: at(-130) },
    ...(row.status === 'delivery_failed'
      ? [{ fromStatus: 'out_for_delivery', toStatus: 'delivery_failed', actor: 'store', reason: 'customer_unreachable', at: at(-125) }]
      : []),
  ],
  serverNow: new Date().toISOString(),
});

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

async function fakeApi(page: Page, role: string) {
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role, stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (url.pathname.endsWith('/stream')) return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (url.pathname === `${base}/orders`) {
      const tab = url.searchParams.get('status') ?? 'new';
      const items = tab === 'out_for_delivery' ? [outRow()] : tab === 'returns' ? [failedRow()] : [];
      return ok(route, { items, nextCursor: null, serverNow: new Date().toISOString() });
    }
    if (url.pathname === `${base}/orders/${OUT}`) return ok(route, detail(outRow(), PHONE));
    if (url.pathname === `${base}/orders/${FAILED}`) return ok(route, detail(failedRow(), null));
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }) });
  });
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
  await page.setViewportSize({ width: 360, height: 760 });
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

/** No element is wider than the 360 px screen. */
async function fits(page: Page) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(over, 'horizontal overflow at 360 px').toBeLessThanOrEqual(0);
}

test('360 px: the Out for delivery card shows the full phone to call (B-04, BK-27)', async ({ page }) => {
  await fakeApi(page, 'cashier');
  await page.goto('/orders');
  await page.getByRole('tab', { name: /Out for delivery/ }).click();
  const card = page.getByRole('article', { name: 'TZ-1042' });
  await expect(card.getByRole('link', { name: `Call ${PHONE}` })).toBeVisible();
  await fits(page);
  await shot(page, 'w2-02-delivery-out-card-360.png');
});

test('360 px: Returns to confirm lists the time since the failure (B-04, BK-26)', async ({ page }) => {
  await fakeApi(page, 'cashier');
  await page.goto('/orders');
  await page.getByRole('tab', { name: /Returns to confirm/ }).click();
  await expect(page.getByRole('article', { name: 'TZ-1043' })).toContainText(/Failed 2 h \d+ min ago/);
  await fits(page);
  await shot(page, 'w2-02-delivery-returns-card-360.png');
});

test('360 px: a cashier confirms the goods came back and is told to ask a manager for damage (B-04, BK-26)', async ({ page }) => {
  await fakeApi(page, 'cashier');
  await page.goto(`/orders/${FAILED}`);
  const panel = page.getByRole('region', { name: 'Did the goods come back?' });
  await expect(panel.getByRole('button', { name: 'Yes, all back' })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'No, damaged' })).toHaveCount(0);
  await expect(panel.getByText('Ask a manager to record damage')).toBeVisible();
  await panel.scrollIntoViewIfNeeded();
  await fits(page);
  await shot(page, 'w2-02-delivery-return-cashier-360.png');
});

test('360 px: a manager sees both return answers (B-04, BK-26)', async ({ page }) => {
  await fakeApi(page, 'manager');
  await page.goto(`/orders/${FAILED}`);
  const panel = page.getByRole('region', { name: 'Did the goods come back?' });
  await expect(panel.getByRole('button', { name: 'No, damaged' })).toBeVisible();
  await panel.scrollIntoViewIfNeeded();
  await fits(page);
  await shot(page, 'w2-02-delivery-return-manager-360.png');
});

test('360 px: an order out for delivery: address, call, code and Delivery failed (B-04, BK-24, BK-27)', async ({ page }) => {
  await fakeApi(page, 'cashier');
  await page.goto(`/orders/${OUT}`);
  await expect(page.getByRole('heading', { name: "Deliver with the customer's code" })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delivery failed' })).toBeVisible();
  await fits(page);
  await shot(page, 'w2-02-delivery-out-detail-360.png');
});
