import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { INSIGHTS, overview, PERIOD, SERVER_NOW } from '../src/test/insightsData';

// D-274 seller auto-onboarding on the production-shaped build with a faked API (no stack): B-02's
// dark green step rail on desktop and phone, the live state with the calm payout note, and the
// dark green shell sidebar. Fails on any sideways scroll at phone width.
// `TZ_QA_DIR=docs/qa/seller-auto-onboarding pnpm exec playwright test onboarding.shots.spec.ts`
// refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const biz = `/v1/biz/businesses/${BID}`;

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
    'access-control-expose-headers': 'ETag',
  };
};

const ok = (route: Route, body: unknown, extra: Record<string, string> = {}) =>
  route.fulfill({ status: 200, contentType: 'application/json', headers: { ...cors(route), ...extra }, body: JSON.stringify(body) });

const problem = (route: Route, status: number, code: string) =>
  route.fulfill({ status, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: `about:blank#${code}`, title: code, status, code }) });

const STORE = {
  id: SID,
  businessId: BID,
  slug: 'sri-lakshmi-indiranagar',
  shortCode: 'IND1',
  name: 'Sri Lakshmi Stores, Indiranagar',
  addressLine1: '12, 100 Feet Road',
  addressLine2: null,
  landmark: 'Opposite the metro',
  locality: 'Indiranagar',
  city: 'Bengaluru',
  pincode: '560038',
  location: { lat: 12.9719, lng: 77.6412 },
  phone: '+919800000000',
  hours: { weekly: {}, holidays: [] },
  status: 'draft',
  openNow: false,
  createdAt: '2026-10-27T04:00:00.000Z',
};

const SETTINGS = {
  hours: { weekly: { mon: [{ open: '09:00', close: '21:00' }], tue: [{ open: '09:00', close: '21:00' }] }, holidays: [] },
  closedUntil: null,
  acceptsPickup: true,
  acceptsDelivery: false,
  deliveryRadiusM: 3000,
  deliveryFeePaise: 3000,
  deliveryFreeAbovePaise: null,
  deliveryMinOrderPaise: 10000,
  deliveryHours: null,
  cashbackBp: 500,
  autoAccept: false,
  asksMuted: false,
  openNow: false,
  inStoreOnly: false,
};

/** The shop as B-02 reads it: `stage` picks how far the owner has got. */
async function fakeApi(page: Page, stage: 'started' | 'live') {
  const live = stage === 'live';
  const business = {
    id: BID,
    brandName: 'Sri Lakshmi Stores',
    legalName: null,
    categoryId: live ? 'c1' : null,
    status: live ? 'verified' : 'draft',
    checkStatus: 'unchecked',
    referralCode: 'SRI1',
    myRole: 'owner',
    submittedAt: live ? '2026-10-27T05:00:00.000Z' : null,
    verifiedAt: live ? '2026-10-27T05:00:00.000Z' : null,
    infoRequest: null,
    createdAt: '2026-10-27T04:00:00.000Z',
  };
  const store = { ...STORE, status: live ? 'live' : 'draft' };
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname;
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (p === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (p === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: business.brandName, status: business.status, role: 'owner', stores: [{ id: SID, name: store.name, slug: store.slug, status: store.status }] }],
        invitations: [],
        nextCursor: null,
      });
    }
    if (p === biz) return ok(route, business, { etag: '"v1"' });
    if (p === `${biz}/stores`) return ok(route, { items: [store], nextCursor: null });
    if (p === `${biz}/stores/${SID}/settings`) return ok(route, SETTINGS, { etag: '"v1"' });
    if (p === `${biz}/kyc`) return ok(route, { status: 'missing', panLast4: null, gstinLast4: null, upiIdLast4: null, bankAccountLast4: null });
    if (p === `${biz}/documents`) return ok(route, { items: [] });
    if (p === '/v1/discover/categories') {
      return ok(route, {
        items: [
          { id: 'c1', name: 'Grocery', slug: 'grocery', parentId: null },
          { id: 'c2', name: 'Pharmacy', slug: 'pharmacy', parentId: null },
          { id: 'c3', name: 'Dairy and bakery', slug: 'dairy', parentId: null },
          { id: 'c4', name: 'Fruits and vegetables', slug: 'fruits', parentId: null },
          { id: 'c5', name: 'Stationery', slug: 'stationery', parentId: null },
          { id: 'c6', name: 'Home and kitchen', slug: 'home', parentId: null },
        ],
      });
    }
    if (p === `${biz}/stores/${SID}/overview`) return ok(route, overview({ newOrders: [] }));
    if (p === `${biz}/stores/${SID}/insights`) return ok(route, { items: INSIGHTS, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    if (p === `${biz}/stores/${SID}/coach/cards`) return ok(route, { items: [], nextCursor: null, serverNow: SERVER_NOW });
    if (p === `${biz}/stores/${SID}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: new Date().toISOString() });
    return problem(route, 404, 'not_found');
  });
}

const noSidewaysScroll = async (page: Page) =>
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

test('desktop: the B-02 step rail, a step card and the live state with the payout note (D-274)', async ({ page }) => {
  await fakeApi(page, 'started');
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/onboarding');
  const rail = page.getByRole('navigation', { name: 'Setup steps' });
  await expect(rail).toBeVisible();
  await rail.getByRole('button', { name: /What you sell/ }).click();
  await expect(page.getByRole('main').getByRole('heading', { level: 2 })).toBeVisible();
  await shot(page, 'onboarding-desktop.png');

  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await fakeApi(page, 'live');
  await page.reload();
  await expect(page.getByText(/Payouts start after our team checks your shop/)).toBeVisible();
  await shot(page, 'onboarding-live-desktop.png');
});

test('phone: the rail is a dark header with the steps in a row, nothing scrolls sideways (D-274)', async ({ page }) => {
  await fakeApi(page, 'started');
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto('/onboarding');
  await expect(page.getByRole('navigation', { name: 'Setup steps' })).toBeVisible();
  await noSidewaysScroll(page);
  await shot(page, 'onboarding-phone.png');
});

test('desktop: the shell sidebar is dark green (D-274)', async ({ page }) => {
  await fakeApi(page, 'live');
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page.getByRole('navigation').filter({ hasText: 'Overview' }).first()).toBeVisible();
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
  await shot(page, 'sidebar-desktop.png');
});
