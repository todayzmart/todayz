import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// W2-09 screenshots at 390x844 from `vite preview` with a synthetic API (page.route), no stack.
// Files go to the git-ignored test-results/screenshots/ (TZ_QA_DIR changes that on purpose).
const PHONE = { width: 390, height: 844 } as const;
void VIEWPORTS;

const NOW = '2026-10-23T10:00:00.000Z';
const at = (min: number) => new Date(Date.parse(NOW) + min * 60_000).toISOString();
const STORE = { id: '01J9ZSTOREAAAAAAAAAAAAAAAA', slug: 'raju-kirana', name: 'Raju Kirana', addressLine: '12 Main Road', locality: 'Indiranagar' };
const ORDER_ID = '01J9ZORDERAAAAAAAAAAAAAAAA';
const ITEMS = [
  { id: '01J9ZITEMAAAAAAAAAAAAAAAA1', name: 'Toor dal', pack: '1 kg', unit: 8400, qty: 2 },
  { id: '01J9ZITEMAAAAAAAAAAAAAAAA2', name: 'Sunflower oil', pack: '1 l', unit: 14500, qty: 1 },
];

const quoteBody = {
  storeId: STORE.id,
  fulfilment: 'pickup',
  lines: ITEMS.map((i) => ({
    storeItemId: i.id,
    productId: i.id.replace('ITEM', 'PROD'),
    name: i.name,
    variant: null,
    packSize: 1,
    unit: i.pack.endsWith('kg') ? 'kg' : 'l',
    imageKey: null,
    quantity: i.qty,
    availableQuantity: 20,
    availability: 'in_stock',
    unitPricePaise: i.unit,
    mrpPaise: i.unit,
    lineSubtotalPaise: i.unit * i.qty,
    discountPaise: 0,
    lineTotalPaise: i.unit * i.qty,
    priceSource: 'catalog',
    validUntil: null,
  })),
  subtotalPaise: 31300,
  discountPaise: 0,
  itemSubtotalPaise: 31300,
  itemsAfterOffersPaise: 31300,
  deliveryFeePaise: 0,
  creditsRequested: 0,
  creditsAllowed: 0,
  creditsUsed: 0,
  creditsLimit: null,
  totalPaise: 31300,
  offersApplied: [],
  orderable: true,
  validUntil: null,
  serverNow: NOW,
};

const orderBody = (over: Record<string, unknown> = {}) => ({
  id: ORDER_ID,
  number: 'R-1042',
  store: STORE,
  fulfilment: 'pickup',
  status: 'placed',
  statusReason: null,
  statusNote: null,
  lines: ITEMS.map((i) => ({
    id: i.id.replace('ITEM', 'LINE'),
    storeItemId: i.id,
    name: i.name,
    variant: null,
    packSize: 1,
    unit: i.pack.endsWith('kg') ? 'kg' : 'l',
    imageKey: null,
    unitPricePaise: i.unit,
    quantityRequested: i.qty,
    quantityAccepted: null,
    lineTotalPaise: i.unit * i.qty,
    priceSource: 'catalog',
  })),
  subtotalPaise: 31300,
  discountPaise: 0,
  itemSubtotalPaise: 31300,
  itemsAfterOffersPaise: 31300,
  creditsUsed: 0,
  deliveryFeePaise: 0,
  totalPaise: 31300,
  cashbackCredits: 0,
  paymentStatus: 'pay_at_shop',
  paymentMethod: null,
  code: null,
  codeLocked: false,
  canExtend: false,
  askReplyId: null,
  acceptBy: at(4),
  partialConfirmBy: null,
  holdUntil: null,
  extendedAt: null,
  placedAt: NOW,
  acceptedAt: null,
  collectedAt: null,
  timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW }],
  version: 1,
  serverNow: NOW,
  ...over,
});

const acceptedOrder = () =>
  orderBody({
    status: 'accepted',
    code: '4821',
    acceptBy: null,
    holdUntil: at(30),
    canExtend: true,
    acceptedAt: at(1),
    timeline: [
      { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW },
      { fromStatus: 'placed', toStatus: 'accepted', actor: 'staff', reason: null, at: at(1) },
    ],
    version: 2,
  });

const summary = (id: string, status: string) => ({
  id,
  number: `R-${id.slice(-4)}`,
  store: STORE,
  fulfilment: 'pickup',
  status,
  statusReason: null,
  itemCount: 3,
  totalPaise: 31300,
  acceptBy: null,
  holdUntil: null,
  placedAt: NOW,
  version: 1,
});

let guard: PageGuard;

async function api(page: Page, order: () => unknown, me?: unknown) {
  const origin = 'http://127.0.0.1';
  void origin;
  await page.route('**/v1/**', (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const h = req.headers();
    const cors = {
      'access-control-allow-origin': h['origin'] ?? '*',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': '*',
      'access-control-allow-methods': '*',
    };
    const send = (status: number, body: unknown) =>
      route.fulfill({ status, contentType: status >= 400 ? 'application/problem+json' : 'application/json', headers: cors, body: JSON.stringify(body) });
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors });
    const p = url.pathname;
    if (p === '/v1/auth/refresh')
      return send(200, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    if (p === '/v1/me' && me) return send(200, me);
    if (p === '/v1/me/checkout/quote') return send(200, quoteBody);
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors, body: '' });
    if (p === '/v1/me/orders') {
      const active = url.searchParams.get('status') === 'active';
      return send(200, {
        items: active ? [summary('01J9ZORDERAAAAAAAAAAAA0001', 'accepted')] : [summary('01J9ZORDERAAAAAAAAAAAA0002', 'collected'), summary('01J9ZORDERAAAAAAAAAAAA0003', 'rejected')],
        nextCursor: null,
        serverNow: NOW,
      });
    }
    if (p.startsWith('/v1/me/orders/')) return send(200, order());
    return send(404, { type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' });
  });
}

/** Writes the cart where the app reads it (IndexedDB), before any page script runs. */
async function seedCart(page: Page) {
  await page.addInitScript(
    ({ store, items }) => {
      const state = {
        store: { id: store.id, slug: store.slug, name: store.name },
        lines: items.map((i: { id: string; name: string; pack: string; unit: number; qty: number }) => ({
          storeItemId: i.id,
          productName: i.name,
          packLabel: i.pack,
          pricePaise: i.unit,
          mrpPaise: i.unit,
          qty: i.qty,
        })),
      };
      const req = indexedDB.open('todayz-customer', 1);
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains('cart')) req.result.createObjectStore('cart');
      };
      req.onsuccess = () => {
        req.result.transaction('cart', 'readwrite').objectStore('cart').put(state, 'current');
      };
    },
    { store: STORE, items: ITEMS },
  );
}

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.customer', '1'));
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

test('cart', async ({ page }) => {
  await api(page, () => orderBody());
  await seedCart(page);
  await page.goto('/cart');
  await expect(page.getByRole('button', { name: /Checkout/ })).toBeVisible();
  await expect(page.getByText('Toor dal')).toBeVisible();
  await shot(page, 'w2-09-cart.png');
});

test('checkout', async ({ page }) => {
  await api(page, () => orderBody());
  await seedCart(page);
  await page.goto('/checkout');
  await expect(page.getByRole('button', { name: /^Place order/ })).toBeVisible();
  await shot(page, 'w2-09-checkout.png');
});

test('tracking: waiting for the shop, then the pickup code', async ({ page }) => {
  let current: () => unknown = () => orderBody();
  await api(page, () => current());
  await page.goto(`/orders/${ORDER_ID}`);
  await expect(page.getByText(/Waiting for Raju Kirana to accept · \d+:\d{2} left/)).toBeVisible();
  await shot(page, 'w2-09-tracking-waiting.png');

  current = acceptedOrder;
  await page.goto(`/orders/${ORDER_ID}`);
  await expect(page.getByRole('group', { name: 'Pickup code' })).toBeVisible();
  await expect(page.getByText(/Held for you/)).toBeVisible();
  await shot(page, 'w2-09-tracking-code.png');
  await shot(page, 'w2-09-tracking-code-full.png', { fullPage: true });
});

test('tracking: the shop declined', async ({ page }) => {
  await api(page, () => orderBody({ status: 'rejected', statusReason: 'out_of_stock', acceptBy: null, version: 2 }));
  await page.goto(`/orders/${ORDER_ID}`);
  await expect(page.getByText('Raju Kirana could not take this order')).toBeVisible();
  await shot(page, 'w2-09-tracking-declined.png');
});

test('orders list', async ({ page }) => {
  await api(page, () => orderBody());
  await page.goto('/orders');
  await expect(page.getByRole('region', { name: 'In progress' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Earlier' })).toBeVisible();
  await shot(page, 'w2-09-orders.png');
});

// W2-02 finish: the partial confirm (BK-09) and the C-21 order limit (BK-16).
test('tracking: the shop can give part of the order', async ({ page }) => {
  const [dal, oil] = orderBody().lines;
  await api(page, () =>
    orderBody({
      status: 'awaiting_customer',
      acceptBy: null,
      partialConfirmBy: at(4),
      lines: [
        { ...dal, quantityAccepted: 1, lineTotalPaise: 8400 },
        { ...oil, quantityAccepted: 1 },
      ],
      subtotalPaise: 22900,
      itemSubtotalPaise: 22900,
      itemsAfterOffersPaise: 22900,
      creditsUsed: 10,
      totalPaise: 21900,
      timeline: [
        { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW },
        { fromStatus: 'placed', toStatus: 'awaiting_customer', actor: 'staff', reason: null, at: at(1) },
      ],
      version: 2,
    }),
  );
  await page.goto(`/orders/${ORDER_ID}`);
  await expect(page.getByRole('button', { name: /^Take it/ })).toBeVisible();
  await expect(page.getByText('Credits used')).toBeVisible();
  await shot(page, 'w2-02-partial-confirm.png');
});

test('checkout: the order limit after missed pickups', async ({ page }) => {
  // The banner hides once `until` is past the time `/v1/me` was read, so the date follows the real clock.
  const until = new Date(Date.now() + 7 * 24 * 60 * 60_000).toISOString();
  const me = {
    id: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
    name: null,
    preferredLang: 'en',
    phoneMasked: '90•••••011',
    app: 'customer',
    customer: { referralCode: 'ABC123', activeOrderLimit: { maxActiveOrders: 1, until, reason: 'no_show_strikes' } },
    createdAt: NOW,
  };
  await api(page, () => orderBody(), me);
  await seedCart(page);
  await page.goto('/checkout');
  await expect(page.getByText('Ordering is limited for now')).toBeVisible();
  await shot(page, 'w2-02-limit-banner.png');
});
