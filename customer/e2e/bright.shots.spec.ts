// D-249 bright skin: phone screenshots of each restyled screen, on the e2e build with a faked API (no stack).
// Runs only with TZ_QA_DIR, e.g. `TZ_QA_DIR=docs/qa/D-249 pnpm exec playwright test e2e/bright.shots.spec.ts`.
// Add TZ_AXE=<path to axe-core/axe.min.js> to fail on accessibility violations or sideways scroll.
import { shot } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the screenshots');

const PHONE = { width: 360, height: 780 } as const;
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const NOW = new Date().toISOString();

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
  };
};
const ok = (route: Route, body: unknown, headers: Record<string, string> = {}) =>
  route.fulfill({ status: 200, contentType: 'application/json', headers: { ...cors(route), ...headers }, body: JSON.stringify(body) });

const addresses = [
  { addressId: '01J9ZADDRAAAAAAAAAAAAAAAA1', label: 'Home', line1: '12, 4th Cross', line2: 'Indiranagar', landmark: 'Near the park', pincode: '560038', lat: 12.97, lng: 77.64, isDefault: true, updatedAt: NOW, version: 1 },
  { addressId: '01J9ZADDRAAAAAAAAAAAAAAAA2', label: 'Office', line1: '3rd floor, 80 Feet Road', line2: 'Koramangala', landmark: null, pincode: '560034', lat: 12.93, lng: 77.62, isDefault: false, updatedAt: NOW, version: 1 },
];

// Trial photos (CC BY 2.0, design/trials/photos/CREDITS.md) stand in for shop photos in these shots only.
const PHOTOS = fileURLToPath(new URL('../../design/trials/photos/', import.meta.url));

const shop = (slug: string, name: string, over: Record<string, unknown> = {}) => ({
  storeId: `01KE0S1W6W8ZV7T1C3A6MS${slug.slice(0, 4).toUpperCase().padEnd(4, '0')}`,
  storeSlug: slug,
  name,
  locality: 'Indiranagar',
  distanceM: 450,
  open: true,
  pickup: true,
  delivery: true,
  bookable: true,
  rating: 4.6,
  ratingCount: 120,
  cashbackPct: 0,
  categoryTint: 'green',
  photoKey: null,
  ...over,
});

const DISCOVER: Record<string, unknown> = {
  '/v1/discover/places/search': { items: [{ label: '560038', lat: 12.97, lng: 77.64 }] },
  '/v1/discover/summary': { storesNearby: 14, openNow: 9, productsInRadius: 1280, pins: [], pinsTruncated: false, serverNow: NOW },
  '/v1/discover/categories': {
    items: [
      ['grocery', 'Grocery', 'green', 'shopping-bag'],
      ['pharmacy', 'Pharmacy', 'rose', 'pill'],
      ['beauty', 'Beauty', 'lilac', 'sparkles'],
      ['mobiles', 'Mobiles', 'sky', 'smartphone'],
      ['stationery', 'Stationery', 'amber', 'pen'],
      ['hardware', 'Hardware', 'peach', 'wrench'],
    ].map(([slug, name, tint, icon], i) => ({ id: `01KE0S1W6W8ZV7T1C3A6CAT00${i}`, slug, name, names: {}, tint, icon, children: [] })),
  },
  '/v1/discover/stores': {
    items: [
      shop('sri-lakshmi-stores', 'Sri Lakshmi Provision Stores', { photoKey: 'shop-grocery.jpg', cashbackPct: 5 }),
      shop('apollo-pharmacy', 'Jeevan Medicals', { photoKey: 'shop-pharmacy.jpg', categoryTint: 'rose', distanceM: 800, locality: 'HAL 2nd Stage' }),
      shop('annapurna-kirana', 'Annapurna Kirana', { open: false, bookable: false, delivery: false, distanceM: 1200, rating: null, ratingCount: 0, categoryTint: 'amber' }),
    ],
    nextCursor: null,
  },
  '/v1/discover/places/reverse': { name: 'Indiranagar', label: 'Indiranagar, Bengaluru 560038', pincode: '560038', city: 'Bengaluru' },
  '/v1/discover/offers': {
    items: [
      {
        kind: 'banner', storeId: '01KE0S1W6W8ZV7T1C3A6MSSRI-', storeSlug: 'sri-lakshmi-stores', storeName: 'Sri Lakshmi Provision Stores',
        locality: 'Indiranagar', tint: 'green', distanceM: 450, offer: null,
        banner: { headline: 'Fresh fruit, 10% off till 8 pm', imageKey: 'cat-fruit.jpg', template: null },
      },
      {
        kind: 'offer', storeId: '01KE0S1W6W8ZV7T1C3A6MSAPOL', storeSlug: 'apollo-pharmacy', storeName: 'Jeevan Medicals',
        locality: 'HAL 2nd Stage', tint: 'rose', distanceM: 800, offer: { title: 'Flat ₹50 off on ₹499' }, banner: null,
      },
    ],
  },
};

const item = (id: string, name: string, over: Record<string, unknown> = {}) => ({
  storeItemId: `01KE0S1W6W8ZV7T1C3A6ITEM${id}`,
  productId: `01KE0S1W6W8ZV7T1C3A6PROD${id}`,
  name,
  variant: null,
  brand: null,
  packSize: 500,
  unit: 'ml',
  pricePaise: 3000,
  mrpPaise: 3000,
  hasOffer: false,
  inStock: true,
  confirmedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
  imageKey: null,
  ...over,
});

DISCOVER['/v1/discover/search'] = {
  query: 'milk',
  summary: { storesChecked: 12, storesWithIt: 3, minPricePaise: 2800 },
  results: [
    {
      store: shop('sri-lakshmi-stores', 'Sri Lakshmi Provision Stores', { photoKey: 'shop-grocery.jpg', cashbackPct: 5 }),
      items: [
        item('01', 'Nandini Toned Milk', { brand: 'Nandini', pricePaise: 2800, imageKey: 'p-milk.jpg' }),
        item('02', 'Milk Bread', { brand: 'Modern', packSize: 400, unit: 'g', pricePaise: 4500, mrpPaise: 5000, hasOffer: true, imageKey: 'p-bread.jpg' }),
      ],
      badge: 'lowest_price',
      deliveryFeePaise: 2000,
    },
    {
      store: shop('apollo-pharmacy', 'Jeevan Medicals', { photoKey: 'shop-pharmacy.jpg', categoryTint: 'rose', distanceM: 800, locality: 'HAL 2nd Stage' }),
      items: [item('03', 'Dairy Milk Silk', { brand: 'Cadbury', packSize: 60, unit: 'g', pricePaise: 8000, imageKey: 'p-choc.jpg' })],
      badge: 'closest',
      deliveryFeePaise: 0,
    },
    {
      store: shop('annapurna-kirana', 'Annapurna Kirana', { distanceM: 1200, rating: null, ratingCount: 0, categoryTint: 'amber' }),
      items: [item('04', 'Amul Gold Milk', { brand: 'Amul', pricePaise: 3300, confirmedAt: null })],
      badge: null,
      deliveryFeePaise: 0,
    },
  ],
  chips: [],
  refineHint: null,
  nextCursor: null,
  serverNow: NOW,
};
DISCOVER['/v1/discover/search/suggest'] = { items: [] };

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
  const wide = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(wide).toBeLessThanOrEqual(PHONE.width);
}

async function signedIn(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('todayz.lang', 'en');
    localStorage.setItem('todayz.hasSession.customer', '1');
    localStorage.setItem('todayz.place', JSON.stringify({ kind: 'pin', pin: '560038' }));
  });
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    const p = new URL(req.url()).pathname;
    if (p === '/v1/auth/refresh')
      return ok(route, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: USER });
    if (p === '/v1/me') return ok(route, { id: USER, phoneMasked: '+91 ••••• •0021', name: 'Asha', preferredLang: 'en' }, { etag: '"1"' });
    if (p === '/v1/me/addresses') return ok(route, { addresses });
    if (p === '/v1/me/wallet') return ok(route, { availableCredits: 240 });
    if (p in DISCOVER) return ok(route, DISCOVER[p]);
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    return route.fulfill({ status: 404, headers: cors(route), body: '' });
  });
  await page.route('https://img.todayz.localhost/**', (route) =>
    route.fulfill({ status: 200, contentType: 'image/jpeg', body: readFileSync(PHOTOS + new URL(route.request().url()).pathname.split('/').pop()) }),
  );
}

test('account', async ({ page }) => {
  await page.setViewportSize(PHONE);
  await signedIn(page);
  await page.goto('/account');
  await page.getByRole('button', { name: 'Sign out' }).waitFor();
  await page.getByText('Office').waitFor();
  await page.evaluate(() => document.fonts.ready);
  await axe(page);
  await shot(page, 'account.png');
  await shot(page, 'account-full.png', { fullPage: true });
});

test('home', async ({ page }) => {
  await page.setViewportSize(PHONE);
  await signedIn(page);
  await page.goto('/');
  await page.locator('.tz-scard-name', { hasText: 'Annapurna Kirana' }).waitFor();
  await page.getByText('Fresh fruit, 10% off till 8 pm').waitFor();
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => [...document.images].every((i) => i.complete));
  await axe(page);
  await shot(page, 'home.png');
  await shot(page, 'home-full.png', { fullPage: true });
});

test('search', async ({ page }) => {
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => localStorage.setItem('todayz.recentSearches', JSON.stringify(['atta 5 kg', 'paracetamol', 'notebook'])));
  await signedIn(page);
  await page.goto('/search');
  await page.getByText('paracetamol').first().waitFor();
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => [...document.images].every((i) => i.complete));
  await axe(page);
  await shot(page, 'search.png');
});

test('search results', async ({ page }) => {
  await page.setViewportSize(PHONE);
  await signedIn(page);
  await page.goto('/search?q=milk');
  await page.getByText('Amul Gold Milk').waitFor();
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => [...document.images].every((i) => i.complete));
  await axe(page);
  await shot(page, 'search-results.png');
  await shot(page, 'search-results-full.png', { fullPage: true });
});
