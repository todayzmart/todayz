import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, signIn } from './fake-api';

/**
 * A-08 product images (D-252) against the built console: the image queue with an imported image
 * opened beside the product's current one, and a product card with its image. The images are
 * drawn here and served from the local image hosts, so nothing leaves the machine.
 */

const BUTTER = '01JPR0DXCT00000000000000B1';
const TOOR = '01JPR0DXCT00000000000000T1';
const OFF_IMG = '01JPH0T0000000000000000PA1';
const SHOP_IMG = '01JPH0T0000000000000000PB1';

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

/** A simple drawn pack, so the screenshots show a picture without shipping a real photo. */
function pack(label: string, fill: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" viewBox="0 0 400 400">
  <rect width="400" height="400" fill="white"/>
  <rect x="80" y="60" width="240" height="280" rx="24" fill="${fill}"/>
  <text x="200" y="215" font-family="sans-serif" font-size="36" font-weight="700" text-anchor="middle" fill="white">${label}</text>
</svg>`;
}

async function serveImages(page: Page) {
  const reply = (label: string, fill: string) => ({ status: 200, contentType: 'image/svg+xml', body: pack(label, fill) });
  await page.route('https://s3.todayz.localhost/**', (route) =>
    route.fulfill(route.request().url().includes('shop') ? reply('Toor dal', 'darkgoldenrod') : reply('Butter', 'goldenrod')),
  );
  await page.route('https://img.todayz.localhost/**', (route) => route.fulfill(reply('Toor dal', 'sienna')));
}

const image = (over: Record<string, unknown>) => ({
  id: OFF_IMG,
  productId: BUTTER,
  productName: 'Amul Butter 100 g',
  brandName: 'Amul',
  categoryName: 'Dairy',
  productStatus: 'active',
  productImageKey: null,
  source: 'off',
  status: 'pending',
  contentType: 'image/jpeg',
  sizeBytes: 48_000,
  previewUrl: 'https://s3.todayz.localhost/private/off.jpg',
  imageKey: null,
  sourceRef: '8901262010016',
  sourceUrl: 'https://images.openfoodfacts.org/images/products/890/126/201/0016/front_en.jpg',
  pageUrl: 'https://world.openfoodfacts.org/product/8901262010016',
  licence: 'CC BY-SA 3.0',
  attribution: 'Open Food Facts contributors',
  match: 'barcode',
  storeId: null,
  storeName: null,
  rejectReason: null,
  decidedAt: null,
  createdAt: '2026-10-25T05:00:00Z',
  ...over,
});

const product = (over: Record<string, unknown>) => ({
  id: TOOR,
  name: 'Toor Dal',
  brandName: 'Tata Sampann',
  categoryId: '01JCATEG0RY0000000000000S1',
  categoryName: 'Staples',
  variant: null,
  packSize: 1,
  unit: 'kg',
  mrpPaise: 18900,
  barcodes: [],
  status: 'active',
  mergedInto: null,
  storeCount: 14,
  imageKey: `products/${TOOR}/a1.jpg`,
  createdAt: '2026-10-01T09:00:00Z',
  updatedAt: '2026-10-20T09:00:00Z',
  ...over,
});

test('the image queue shows an imported image with its match, licence and credit', async ({ page }) => {
  await signIn(page, ['catalogue']);
  await serveImages(page);
  const queue = [
    image({}),
    image({
      id: SHOP_IMG,
      productId: TOOR,
      productName: 'Toor Dal',
      brandName: 'Tata Sampann',
      categoryName: 'Staples',
      source: 'retailer',
      previewUrl: 'https://s3.todayz.localhost/private/shop.jpg',
      sourceRef: null,
      sourceUrl: null,
      pageUrl: null,
      licence: null,
      attribution: null,
      match: null,
      storeId: '01JST0RE000000000000000S01',
      storeName: 'Annapurna Kirana',
    }),
  ];
  await answerWith(page, '/v1/admin/product-images*', (req) => (req.method() === 'GET' ? { status: 200, body: { items: queue, nextCursor: null, serverNow: NOW } } : undefined));
  await page.goto(`/catalogue?tab=images&open=${OFF_IMG}`);
  const card = page.getByRole('region', { name: 'Amul Butter 100 g' });
  await expect(card.getByText('Same barcode')).toBeVisible();
  await expect(card.getByRole('link', { name: 'Source page' })).toHaveAttribute('href', 'https://world.openfoodfacts.org/product/8901262010016');
  await expect(card.getByRole('img', { name: 'Amul Butter 100 g, the image waiting for review' })).toBeVisible();
  await page.getByRole('checkbox', { name: 'Choose every image shown' }).check();
  await expect(page.getByRole('button', { name: 'Approve chosen (2)' })).toBeEnabled();
  await shot(page, 'admin-a08-image-queue.png', { fullPage: true });
});

test('an admin sees the product image and can replace it', async ({ page }) => {
  await signIn(page, ['admin']);
  await serveImages(page);
  const items = [product({}), product({ id: BUTTER, name: 'Amul Butter', brandName: 'Amul', imageKey: null, categoryName: 'Dairy' })];
  await answerWith(page, '/v1/admin/products*', (req) => (req.method() === 'GET' ? { status: 200, body: { items, nextCursor: null, serverNow: NOW } } : undefined));
  await answerWith(page, `/v1/admin/products/${TOOR}`, (req) => (req.method() === 'GET' ? { status: 200, body: items[0] } : undefined));
  await page.goto(`/catalogue?open=${TOOR}`);
  await expect(page.getByRole('img', { name: 'Toor Dal, the current image' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Replace image' })).toBeVisible();
  await expect(page.getByText('JPEG, PNG or WebP, up to 5 MB. It goes live at once.')).toBeVisible();
  await shot(page, 'admin-a08-product-image.png', { fullPage: true });
});
