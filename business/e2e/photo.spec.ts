import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { overview } from '../src/test/insightsData';

// B-07 product photos (D-252) on the production-shaped build with a faked API: the stock list with
// an approved image, the shop's own photo waiting for review and the initials, and a new product
// with a photo picked. The pictures are drawn here and served from the local image hosts.
// `TZ_QA_DIR=docs/qa/product-images pnpm exec playwright test photo.spec.ts` refreshes the shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const NOW = () => new Date().toISOString();
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;
const CATEGORY = { id: '01J9ZCCCCCCCCCCCCCCCCCCCCC', slug: 'grocery', name: 'Grocery', names: {}, tint: 'mint', icon: 'shopping-basket', children: [] };

const id = (p: string, n: number) => `01J9Z${p.repeat(19)}${String(n).padStart(2, '0')}`;

function item(n: number, name: string, over: Record<string, unknown> = {}) {
  return {
    id: id('I', n),
    storeId: SID,
    productId: id('P', n),
    name,
    productName: name,
    localName: null,
    brand: null,
    variant: null,
    packSize: 1,
    unit: 'kg',
    categoryId: CATEGORY.id,
    barcodes: [],
    pricePaise: 5800,
    mrpPaise: null,
    productMrpPaise: null,
    onHand: 12,
    reserved: 0,
    available: 12,
    lowStockAt: null,
    lowStock: false,
    isListed: true,
    stockUpdatedAt: NOW(),
    stale: false,
    imageKey: null,
    photo: null,
    createdAt: NOW(),
    updatedAt: NOW(),
    ...over,
  };
}

const ITEMS = [
  item(1, 'Amul Butter', { packSize: 100, unit: 'g', pricePaise: 5800, imageKey: `products/${id('P', 1)}/a1.jpg` }),
  item(2, 'Jaggery', { packSize: 500, unit: 'g', pricePaise: 6500, photo: { id: 'ph2', status: 'pending', previewUrl: 'https://s3.todayz.localhost/private/jaggery.jpg', rejectReason: null } }),
  item(3, 'Toor dal', { pricePaise: 18900 }),
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

function pack(label: string, fill: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" viewBox="0 0 400 400">
  <rect width="400" height="400" fill="white"/>
  <rect x="80" y="60" width="240" height="280" rx="24" fill="${fill}"/>
  <text x="200" y="215" font-family="sans-serif" font-size="40" font-weight="700" text-anchor="middle" fill="white">${label}</text>
</svg>`;
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
    if (url.pathname === '/v1/discover/categories') return ok(route, { items: [CATEGORY], nextCursor: null });
    if (url.pathname === `${base}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: NOW() });
    if (url.pathname === `${base}/overview`) return ok(route, overview({ newOrders: [] }));
    if (url.pathname === `${base}/items`) return ok(route, { items: ITEMS, nextCursor: null });
    // Voice update is on for the shop, as on the stack; its link sits on the stock screen.
    if (url.pathname === `${base}/ai/voice-stock`) return ok(route, { maxSeconds: 15, maxBytes: 1_048_576, languages: ['en-IN'] });
    if (url.pathname === `/v1/biz/businesses/${BID}/products/search`) return ok(route, { items: [], nextCursor: null });
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }) });
  });
  const image = (label: string, fill: string) => ({ status: 200, contentType: 'image/svg+xml', body: pack(label, fill) });
  await page.route('https://img.todayz.localhost/**', (route) => route.fulfill(image('Butter', 'goldenrod')));
  await page.route('https://s3.todayz.localhost/**', (route) => route.fulfill(image('Jaggery', 'saddlebrown')));
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
  await fakeApi(page);
  await page.setViewportSize({ width: 360, height: 800 });
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('phone 360: the stock list shows the approved image, the photo waiting for review, or the initials', async ({ page }) => {
  await page.goto('/stock');
  await expect(page.getByText('TD', { exact: true })).toBeVisible();
  const srcs = await page.locator('main img').evaluateAll((els) => els.map((e) => e.getAttribute('src')));
  expect(srcs).toEqual([`https://img.todayz.localhost/products/${id('P', 1)}/a1.jpg`, 'https://s3.todayz.localhost/private/jaggery.jpg']);
  await shot(page, 'business-b07-stock-photos-360.png', { fullPage: true });
});

test('phone 360: a new product with a photo picked from the camera or files', async ({ page }) => {
  await page.goto('/stock');
  await page.getByRole('button', { name: 'Add item' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('searchbox').fill('Groundnut oil');
  await dialog.getByRole('button', { name: 'Create a new product' }).click();
  await dialog.getByLabel('Product name').fill('Groundnut oil');
  await dialog.getByLabel('Pack size').fill('1');
  await dialog.getByLabel('Unit').selectOption('l');
  await dialog.getByLabel('Category').selectOption(CATEGORY.id);
  // A real PNG, drawn in the page, so the browser shrinks it to a JPEG as on a phone.
  const png = await page.evaluate(async () => {
    const c = document.createElement('canvas');
    c.width = 600;
    c.height = 800;
    const g = c.getContext('2d');
    if (!g) return '';
    g.fillStyle = 'goldenrod';
    g.fillRect(0, 0, 600, 800);
    g.fillStyle = 'white';
    g.font = 'bold 72px sans-serif';
    g.fillText('Oil', 230, 420);
    return c.toDataURL('image/png').split(',')[1] ?? '';
  });
  await dialog.getByLabel('Take or choose a photo').setInputFiles({ name: 'oil.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') });
  await expect(dialog.getByRole('button', { name: 'Remove photo' })).toBeVisible();
  const preview = dialog.getByRole('img', { name: 'The photo you picked' });
  await expect(preview).toBeVisible();
  await preview.scrollIntoViewIfNeeded();
  await shot(page, 'business-b07-new-product-photo-360.png', { fullPage: true });
});
