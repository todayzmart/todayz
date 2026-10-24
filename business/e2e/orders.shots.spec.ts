import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-04 Accept part (W2-02 finish, BK-08) on the production-shaped build with a faked API (no stack).
// `TZ_QA_DIR=docs/qa/W2-02-finish pnpm exec playwright test orders.shots.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const OID = '01J9ZOOOOOOOOOOOOOOOOOOOO1';
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;
const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();

const summary = () => ({
  id: OID,
  number: 'TZ-1042',
  customerName: 'Meera',
  fulfilment: 'pickup',
  status: 'placed',
  statusReason: null,
  itemCount: 3,
  totalPaise: 52100,
  acceptBy: at(4),
  holdUntil: null,
  placedAt: at(-1),
  seen: true,
  version: 1,
});

const line = (n: number, name: string, packSize: number, unit: string, unitPricePaise: number, quantityRequested: number, available: number) => ({
  id: `01J9ZLLLLLLLLLLLLLLLLLLLL${n}`,
  storeItemId: `01J9ZIIIIIIIIIIIIIIIIIIII${n}`,
  name,
  variant: null,
  packSize,
  unit,
  imageKey: null,
  unitPricePaise,
  quantityRequested,
  quantityAccepted: null,
  lineTotalPaise: unitPricePaise * quantityRequested,
  priceSource: 'base',
  available,
});

const order = () => ({
  ...summary(),
  customer: { name: 'Meera', phoneMasked: '+91 98xxx xx210' },
  statusNote: null,
  lines: [line(1, 'Amul Butter', 100, 'g', 5800, 2, 9), line(2, 'Toor dal', 1, 'kg', 12900, 2, 1), line(3, 'Sunflower oil', 1, 'l', 14500, 1, 0)],
  subtotalPaise: 52100,
  discountPaise: 0,
  itemSubtotalPaise: 52100,
  itemsAfterOffersPaise: 52100,
  creditsUsed: 0,
  deliveryFeePaise: 0,
  totalPaise: 52100,
  paymentStatus: 'pay_at_store',
  paymentMethod: null,
  codeLocked: false,
  codeAttemptsLeft: 5,
  partialConfirmBy: null,
  extendedAt: null,
  acceptedAt: null,
  collectedAt: null,
  timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: at(-1) }],
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
    if (url.pathname.endsWith('/stream')) return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (url.pathname === `${base}/orders`) {
      const tab = url.searchParams.get('status') ?? 'new';
      return ok(route, { items: tab === 'new' ? [summary()] : [], nextCursor: null, serverNow: new Date().toISOString() });
    }
    if (url.pathname === `${base}/orders/${OID}`) return ok(route, order());
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
});

test('desktop: a short order offers Accept part (B-04, BK-08)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/orders');
  await expect(page.getByText(/Accept part, reject the order, or update the stock/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Accept part of order TZ-1042' })).toBeEnabled();
  await shot(page, 'w2-02-accept-part-card.png');
});

test('phone: the Accept part sheet, prefilled with what the stock covers (B-04, BK-08)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.getByRole('button', { name: 'Accept part of order TZ-1042' }).click();
  const sheet = page.getByRole('dialog', { name: 'Accept part of TZ-1042' });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByText('Asked 2 · 1 in stock')).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Send to customer' })).toBeEnabled();
  await shot(page, 'w2-02-accept-part-sheet.png');
});
