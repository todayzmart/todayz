import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// W3-16 journeys at 390x844 from `vite preview` with a synthetic API (page.route), no stack:
// C-25 "Ask shops nearby" (from a search with no results and from C-08), C-26 progress and replies
// with Book, and the share link /x/{code}. TZ_QA_DIR=docs/qa/W3-16 writes the committed screenshots.
const PHONE = { width: 390, height: 844 } as const;

// 15:30 IST, so ready-by times read like a shop day. Countdowns run from serverNow, not the device clock.
const NOW = '2026-10-23T10:00:00.000Z';
const at = (min: number) => new Date(Date.parse(NOW) + min * 60_000).toISOString();
const ASK_ID = '01KE0S1W6W8ZV7T1C3A6ASK001';
const ITEM_ID = '01KE0S1W6W8ZV7T1C3A6ITEM01';
const PRODUCT_ID = '01KE0S1W6W8ZV7T1C3A6PROD01';
const SHOP = 'sri-lakshmi-stores';
const STORE_ID = '01KE0S1W6W8ZV7T1C3A6MS0001';
const CODE = 'aB3dE5gH7j';
const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/;

const weekly = Object.fromEntries(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, [{ open: '09:00', close: '21:00' }]]));
const storePage = {
  storeId: STORE_ID,
  storeSlug: SHOP,
  name: 'Sri Lakshmi Stores',
  locality: 'Indiranagar',
  addressLine1: null,
  addressLine2: null,
  landmark: null,
  city: 'Bengaluru',
  pincode: '560038',
  lat: 12.97,
  lng: 77.64,
  distanceM: 450,
  open: true,
  closedUntil: null,
  hours: { weekly, holidays: [] },
  pickup: true,
  delivery: false,
  bookable: true,
  rating: 4.6,
  ratingCount: 120,
  cashbackPct: 0,
  categoryTint: 'green',
  categorySlug: 'grocery',
  categoryName: 'Grocery',
  deliveryFeePaise: 0,
  deliveryFreeAbovePaise: null,
  deliveryMinOrderPaise: 0,
  deliveryRadiusM: 0,
  serverNow: NOW,
};
const item = {
  storeItemId: ITEM_ID,
  productId: PRODUCT_ID,
  name: 'Aashirvaad Atta',
  variant: null,
  brand: 'Aashirvaad',
  packSize: 5,
  unit: 'kg',
  pricePaise: 26000,
  mrpPaise: 29500,
  hasOffer: false,
  inStock: false,
  confirmedAt: at(-12),
  imageKey: null,
};

const ask = (over: Record<string, unknown> = {}) => ({
  askId: ASK_ID,
  itemName: 'Aashirvaad Atta',
  productId: PRODUCT_ID,
  quantity: 2,
  needBy: 'today',
  status: 'open',
  closeReason: null,
  progress: { asked: 5, replied: 0, bestPricePaise: null },
  yesCount: 0,
  createdAt: NOW,
  closedAt: null,
  windowEndsAt: at(15),
  version: 1,
  ...over,
});

const reply = (over: Record<string, unknown> = {}) => ({
  askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY01',
  storeId: STORE_ID,
  storeSlug: SHOP,
  storeName: 'Sri Lakshmi Stores',
  distanceM: 600,
  pricePaise: 25500,
  readyBy: at(30),
  repliedAt: at(-4),
  priceValidUntil: at(20),
  storeItemId: ITEM_ID,
  listedPricePaise: 26000,
  bookable: true,
  inStoreOnly: false,
  orderId: null,
  ...over,
});

const REPLIES = [
  reply(),
  reply({ askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY02', storeName: 'Annapurna Kirana', storeSlug: 'annapurna-kirana', storeId: '01KE0S1W6W8ZV7T1C3A6MS0002', distanceM: 1200, pricePaise: null, priceValidUntil: null, readyBy: at(10) }),
  reply({ askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY03', storeName: 'Corner Shop', storeId: '01KE0S1W6W8ZV7T1C3A6MS0003', distanceM: 1800, readyBy: null, inStoreOnly: true, bookable: false }),
];

const preview = {
  storeItemId: ITEM_ID,
  productId: PRODUCT_ID,
  itemName: 'Aashirvaad Atta',
  storeName: 'Sri Lakshmi Stores',
  storeSlug: SHOP,
  locality: 'Indiranagar',
  pricePaise: 26000,
  mrpPaise: 29500,
  imageUrl: null,
};

type Seen = { method: string; path: string; body: unknown; key: string | undefined };
type Answers = { detail?: () => unknown; share?: 'ok' | 'gone' };

let guard: PageGuard;

async function api(page: Page, answers: Answers = {}): Promise<Seen[]> {
  const seen: Seen[] = [];
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
    seen.push({ method: req.method(), path: p, body: req.postDataJSON() as unknown, key: h['idempotency-key'] });
    if (p === '/v1/auth/refresh')
      return send(200, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors, body: '' });
    if (p === '/v1/discover/search')
      return send(200, { query: url.searchParams.get('q') ?? '', summary: { storesChecked: 6, storesWithIt: 0, minPricePaise: null }, results: [], chips: [], refineHint: null, nextCursor: null, serverNow: NOW });
    if (p === `/v1/discover/stores/${SHOP}`) return send(200, storePage);
    if (p === `/v1/discover/stores/${SHOP}/items`) return send(200, { items: [item], nextCursor: null, serverNow: NOW });
    if (p === `/v1/discover/stores/${SHOP}/items/${ITEM_ID}`) return send(200, { item, serverNow: NOW });
    if (p === '/v1/me/asks' && req.method() === 'POST') return send(201, { ask: ask(), replies: [], serverNow: NOW });
    if (p === `/v1/me/asks/${ASK_ID}`) return send(200, answers.detail?.() ?? { ask: ask(), replies: [], serverNow: NOW });
    if (p === `/v1/public/share-cards/${CODE}`)
      return answers.share === 'gone' ? send(410, { type: 'about:blank', title: 'Gone', status: 410, code: 'share_card_unavailable' }) : send(200, preview);
    if (p === '/v1/public/share-cards' && req.method() === 'POST')
      return send(200, { code: CODE, url: `https://app.todayz.in/x/${CODE}`, text: 'Aashirvaad Atta at Sri Lakshmi Stores, ₹260 · TodayZ', imageUrl: null });
    return send(404, { type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' });
  });
  return seen;
}

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => {
    localStorage.setItem('todayz.lang', 'en');
    localStorage.setItem('todayz.hasSession.customer', '1');
    localStorage.setItem('todayz.place', JSON.stringify({ kind: 'gps', lat: 12.97, lng: 77.64 }));
  });
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

test('C-25 from a search with no results: the sheet sends once and opens C-26', async ({ page }) => {
  const seen = await api(page);
  await page.goto('/search?q=ragi%20flour');
  await expect(page.getByText(/ragi flour/).first()).toBeVisible();
  await shot(page, 'customer-c25-search-zero.png');

  await page.getByRole('button', { name: 'Ask shops nearby' }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByText('Shops see the item, the quantity and roughly how far you are. Never your name or number.')).toBeVisible();
  await sheet.getByRole('button', { name: /One more/ }).click();
  await sheet.getByRole('button', { name: 'Today' }).click();
  await shot(page, 'customer-c25-sheet.png');

  await sheet.getByRole('button', { name: 'Send' }).click();
  await expect(page).toHaveURL(new RegExp(`/asks/${ASK_ID}$`));
  const posts = seen.filter((s) => s.method === 'POST' && s.path === '/v1/me/asks');
  expect(posts).toHaveLength(1);
  expect(posts[0]?.body).toMatchObject({ query: 'ragi flour', quantity: 2, needBy: 'today', lat: 12.97, lng: 77.64 });
  expect(posts[0]?.key).toMatch(ULID);
  await expect(page.getByText('Asked 5 shops · 0 replied')).toBeVisible();
  await expect(page.getByText('Waiting for shops. Most reply within 15 min.')).toBeVisible();
  await shot(page, 'customer-c26-waiting.png');
});

test('C-25 from C-08 sends the product id', async ({ page }) => {
  const seen = await api(page);
  await page.goto(`/s/${SHOP}/i/${ITEM_ID}`);
  await page.getByRole('button', { name: 'Ask shops nearby' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Send' }).click();
  await expect(page).toHaveURL(new RegExp(`/asks/${ASK_ID}$`));
  const post = seen.find((s) => s.method === 'POST' && s.path === '/v1/me/asks');
  expect(post?.body).toMatchObject({ productId: PRODUCT_ID, quantity: 1 });
});

test('C-26 replies: soonest badge, prices, in-store only, and Book opens checkout', async ({ page }) => {
  await api(page, { detail: () => ({ ask: ask({ progress: { asked: 5, replied: 3, bestPricePaise: 25500 }, yesCount: 3 }), replies: REPLIES, serverNow: NOW }) });
  await page.goto(`/asks/${ASK_ID}`);
  await expect(page.getByText('Asked 5 shops · 3 replied · best ₹255')).toBeVisible();
  const cards = page.getByRole('article');
  await expect(cards).toHaveCount(3);
  await expect(cards.nth(1).getByText('Ready soonest')).toBeVisible();
  await expect(cards.nth(1).getByText('Ask the shop for the price')).toBeVisible();
  await expect(cards.nth(2).getByText('In-store only: visit the shop')).toBeVisible();
  await expect(cards.nth(2).getByRole('button', { name: 'Book' })).toHaveCount(0);
  await shot(page, 'customer-c26-replies.png');
  await shot(page, 'customer-c26-replies-full.png', { fullPage: true });

  await cards.nth(0).getByRole('button', { name: 'Book' }).click();
  await expect(page).toHaveURL(/\/checkout$/);
});

test('C-26 closed with no Yes', async ({ page }) => {
  await api(page, { detail: () => ({ ask: ask({ status: 'closed', closeReason: 'window_ended', windowEndsAt: null, closedAt: at(-1) }), replies: [], serverNow: NOW }) });
  await page.goto(`/asks/${ASK_ID}`);
  await expect(page.getByText("No shop has it right now. We've told shops nearby people want it.")).toBeVisible();
  await expect(page.getByRole('button', { name: 'Close ask' })).toHaveCount(0);
  await shot(page, 'customer-c26-none.png');
});

test('C-08 Share offers Copy link without a system share sheet', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'share', { value: undefined, configurable: true });
  });
  const seen = await api(page);
  await page.goto(`/s/${SHOP}/i/${ITEM_ID}`);
  await page.getByRole('button', { name: 'Share' }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByRole('button', { name: 'Copy link' })).toBeVisible();
  expect(seen.find((s) => s.path === '/v1/public/share-cards')?.body).toMatchObject({ storeItemId: ITEM_ID });
  await shot(page, 'customer-c08-share-sheet.png');
});

test('share link /x/{code} opens C-08, and a shop that is gone gets a search box', async ({ page }) => {
  await api(page);
  await page.goto(`/x/${CODE}`);
  await expect(page).toHaveURL(new RegExp(`/s/${SHOP}/i/${ITEM_ID}\\?ref=share$`));
  await expect(page.getByRole('heading', { name: /Aashirvaad Atta/ })).toBeVisible();

  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await api(page, { share: 'gone' });
  await page.goto(`/x/${CODE}`);
  await expect(page.getByText("This shop isn't on TodayZ right now")).toBeVisible();
  await shot(page, 'customer-x-gone.png');
  await page.getByLabel('Search for an item').fill('atta');
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page).toHaveURL(/\/search\?q=atta/);
});
