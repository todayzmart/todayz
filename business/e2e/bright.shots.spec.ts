// D-260 bright skin: phone and tablet screenshots of the main business screens, on the e2e build with a faked API (no stack).
// Runs only with TZ_QA_DIR, e.g. `TZ_QA_DIR=docs/qa/D-260 pnpm exec playwright test e2e/bright.shots.spec.ts`.
// Add TZ_AXE=<path to axe-core/axe.min.js> to fail on WCAG 2.2 AA violations (contrast on the grey page among them).
import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { CUSTOMERS, INSIGHTS, overview, PERIOD, SERVER_NOW } from '../src/test/insightsData';

test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the screenshots');

const SIZES = { phone: { width: 360, height: 780 }, tablet: { width: 768, height: 1024 } } as const;
const BIZ = '/v1/biz/businesses/b1';
const BASE = `${BIZ}/stores/s1`;
const NOW = () => new Date().toISOString();
const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();

function item(n: number, name: string, pricePaise: number, available: number, over: Record<string, unknown> = {}) {
  const id = `01J9ZIIIIIIIIIIIIIIIIIII${String(n).padStart(2, '0')}`;
  return {
    id, storeId: 's1', productId: `01J9ZPPPPPPPPPPPPPPPPPPP${String(n).padStart(2, '0')}`, name, productName: name, localName: null, brand: null,
    variant: null, packSize: 1, unit: 'pc', categoryId: 'cat1', barcodes: [], pricePaise, mrpPaise: null, productMrpPaise: null,
    onHand: available, reserved: 0, available, lowStockAt: 5, lowStock: available <= 5, isListed: true, stockUpdatedAt: NOW(), stale: false,
    createdAt: NOW(), updatedAt: NOW(), ...over,
  };
}

const ITEMS = [
  item(1, 'Basmati rice 1 kg', 12000, 24),
  item(2, 'Toor dal 1 kg', 16500, 12),
  item(3, 'Sunflower oil 1 L', 14500, 3),
  item(4, 'Sugar 1 kg', 4800, 30),
  item(5, 'Tea 250 g', 11000, 0),
  item(6, 'Milk 500 ml', 3200, 40),
  item(7, 'Parle-G biscuits', 1000, 80),
  item(8, 'Atta 5 kg', 26000, 2),
];

const orderSummary = (n: number, name: string, status: string, totalPaise: number, items: number) => ({
  id: `01J9ZOOOOOOOOOOOOOOOOOOOO${n}`, number: `TZ-104${n}`, customerName: name, fulfilment: n === 2 ? 'delivery' : 'pickup', status, statusReason: null,
  itemCount: items, totalPaise, acceptBy: at(4 + n), holdUntil: null, placedAt: at(-n), seen: true, version: 1,
});

const offer = (id: string, over: Record<string, unknown> = {}) => ({
  id, storeId: 's1', title: '20% off on dal', kind: 'percent', value: 20, buyQty: null, getQty: null, minBillPaise: null, scope: 'items',
  storeItemIds: [ITEMS[1]?.id], categoryId: null, channels: 'both', perCustomerLimit: null, maxRedemptions: 50, redemptions: 7,
  startsAt: '2026-10-20T08:00:00.000Z', endsAt: null, status: 'live', source: 'manual', batchId: null, version: 3,
  createdAt: '2026-10-20T08:00:00.000Z', updatedAt: '2026-10-20T08:00:00.000Z', ...over,
});

const banner = {
  id: 'bn1', storeId: 's1', headline: 'Fresh fruit, 10% off till 8 pm', imageKey: null, template: { tint: 'green' }, targetType: 'store', targetId: null,
  targetItemIds: [], startsAt: '2026-10-20T08:00:00.000Z', endsAt: null, status: 'live', moderation: 'auto_ok', removeReason: null,
  impressions: 120, clicks: 9, attributedOrders: 2, version: 1, createdAt: '2026-10-20T08:00:00.000Z', updatedAt: '2026-10-20T08:00:00.000Z',
};

const STORE = {
  id: 's1', businessId: 'b1', slug: 'asha-indiranagar', shortCode: 'ASH1', name: 'Asha Stores, Indiranagar', addressLine1: '12 Main Road', addressLine2: null,
  landmark: null, locality: 'Indiranagar', city: 'Bengaluru', pincode: '560038', location: { lat: 12.97, lng: 77.64 }, phone: '+919876543210',
  hours: { weekly: {}, holidays: [] }, status: 'live', openNow: true, createdAt: '2026-10-01T00:00:00.000Z',
};

const SETTINGS = {
  hours: { weekly: { mon: [{ open: '09:00', close: '21:00' }] }, holidays: [] }, closedUntil: null, acceptsPickup: true, acceptsDelivery: true,
  deliveryRadiusM: 3000, deliveryFeePaise: 3000, deliveryFreeAbovePaise: null, deliveryMinOrderPaise: 10000, deliveryHours: null, cashbackBp: 500,
  autoAccept: false, asksMuted: false, openNow: true, inStoreOnly: false,
};

const KYC = { status: 'verified', panLast4: '234F', gstinLast4: null, upiIdLast4: null, bankAccountLast4: '6789', bankIfsc: 'HDFC0001234', legalName: 'Asha Stores' };

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
    'access-control-expose-headers': 'etag',
  };
};
const ok = (route: Route, body: unknown, headers: Record<string, string> = {}) =>
  route.fulfill({ status: 200, contentType: 'application/json', headers: { ...cors(route), ...headers }, body: JSON.stringify(body) });

async function fakeApi(page: Page) {
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (path === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'shots-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (path === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: 'b1', brandName: 'Asha Stores', status: 'verified', role: 'owner', stores: [{ id: 's1', name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
        nextCursor: null,
      });
    }
    if (path.endsWith('/stream')) return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: 'retry: 600000\nevent: ready\ndata: {}\n\n' });
    if (path === `${BASE}/overview`) return ok(route, overview({ range: url.searchParams.get('range') ?? 'today' }));
    if (path === `${BASE}/insights`) return ok(route, { items: INSIGHTS, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    if (path === `${BASE}/coach/cards`) return ok(route, { items: [COACH_CARD], nextCursor: null, serverNow: SERVER_NOW });
    if (path === `${BASE}/asks`) return ok(route, { asks: [], openCount: 0, nextCursor: null, serverNow: SERVER_NOW });
    if (path === `${BASE}/orders`) {
      const tab = url.searchParams.get('status') ?? 'new';
      const items = tab === 'new' ? [orderSummary(1, 'Meera', 'placed', 52100, 3), orderSummary(2, 'Ravi', 'placed', 18400, 2)] : [];
      return ok(route, { items, nextCursor: null, serverNow: NOW() });
    }
    if (path === `${BASE}/items`) return ok(route, { items: ITEMS, nextCursor: null });
    if (path.startsWith(`${BASE}/items/`) && path.endsWith('/batches')) return ok(route, { items: [], nextCursor: null });
    if (path === `${BASE}/batches`) return ok(route, { items: [], nextCursor: null });
    if (path === '/v1/discover/categories') return ok(route, { items: [{ id: 'cat1', slug: 'grocery', name: 'Grocery', names: {}, tint: 'green', icon: 'shopping-bag', children: [] }] });
    if (path === `${BASE}/sync`) return ok(route, { results: [], changes: [], cursor: 1, hasMore: false, serverNow: NOW() });
    if (path === `${BASE}/pos/sales` && req.method() === 'GET') return ok(route, { items: [], nextCursor: null, serverNow: NOW() });
    if (path === `${BASE}/offers`) return ok(route, { items: [offer('o1'), offer('o2', { title: 'Buy 2 get 1 biscuits', kind: 'bxgy', value: 0, buyQty: 2, getQty: 1, status: 'paused', redemptions: 0, maxRedemptions: null })], nextCursor: null });
    if (path === `${BASE}/banners`) return ok(route, { items: [banner], nextCursor: null });
    if (path === `${BASE}/customers`) {
      return ok(route, { segment: url.searchParams.get('segment') ?? 'all', enough: true, total: CUSTOMERS.length, windowDays: 90, lapseDays: 30, items: CUSTOMERS, nextCursor: null, serverNow: SERVER_NOW });
    }
    if (path === BASE) return ok(route, STORE, { etag: '"v2"' });
    if (path === `${BASE}/settings`) return ok(route, SETTINGS, { etag: '"v5"' });
    if (path === `${BIZ}/kyc`) return ok(route, KYC, { etag: '"v3"' });
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

/** One live coach card at the foot of the overview, with the long text that crowded a phone. */
const COACH_CARD = {
  id: 'c1',
  detector: 'missed_orders_rate',
  subjectRef: 'store',
  kind: 'high',
  topic: 'orders',
  title: 'Missed orders are lowering you in search',
  body: '1 of 2 orders in the last 30 days expired unaccepted (50%, limit 30%). Turn on order sound and auto-accept.',
  numbers: { missed: 1, total: 2 },
  action: { type: 'turn_on_auto_accept', payload: { setting: 'auto_accept' } },
  status: 'seen',
  generatedBy: 'rule',
  lang: 'en',
  createdAt: '2026-01-05T00:30:00.000Z',
  validUntil: '2026-01-07T00:30:00.000Z',
  actedAt: null,
  quietUntil: null,
};

async function settle(page: Page) {
  await page.waitForLoadState('networkidle');
  await page.evaluate(() => document.fonts.ready);
}

/** With TZ_AXE (a path to axe.min.js), the screen must have no WCAG 2.2 AA violations. */
async function axe(page: Page) {
  const path = process.env['TZ_AXE'];
  if (!path) return;
  await page.addScriptTag({ path });
  const found = await page.evaluate(async () => {
    const w = window as unknown as { axe: { run: (o: unknown) => Promise<{ violations: { id: string; nodes: { target: string[] }[] }[] }> } };
    const r = await w.axe.run({ runOnly: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] });
    return r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`);
  });
  expect(found).toEqual([]);
}

const tile = (page: Page, name: string) => page.getByRole('region', { name: 'Items' }).getByRole('button', { name: new RegExp(name) });

const SCREENS: [string, string, ((page: Page) => Promise<void>)?][] = [
  ['overview', '/'],
  ['coach', '/', (page) => page.getByRole('region', { name: 'Shop coach' }).scrollIntoViewIfNeeded()],
  [
    'sell',
    '/sell',
    async (page) => {
      await tile(page, 'Basmati rice').click();
      await tile(page, 'Basmati rice').click();
      await tile(page, 'Toor dal').click();
      await tile(page, 'Milk 500 ml').click();
    },
  ],
  ['stock', '/stock'],
  [
    'stock-item',
    '/stock',
    async (page) => {
      await page.getByRole('button', { name: 'Toor dal 1 kg' }).first().click();
      await page.getByRole('dialog').getByRole('button', { name: 'Change price' }).click();
    },
  ],
  ['orders', '/orders'],
  ['offers', '/offers'],
  ['customers', '/customers'],
  ['settings', '/settings'],
];

for (const [size, viewport] of Object.entries(SIZES)) {
  for (const [name, path, act] of SCREENS) {
    test(`${name} on a ${size}`, async ({ page }) => {
      await page.setViewportSize(viewport);
      await page.goto(path);
      await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
      await settle(page);
      if (act) {
        await act(page);
        await settle(page);
      }
      const sideways = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(sideways, 'sideways scroll').toBeLessThanOrEqual(0);
      await shot(page, `${name}-${size}.png`);
      await axe(page);
    });
  }
}
