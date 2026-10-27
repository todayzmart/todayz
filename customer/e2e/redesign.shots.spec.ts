// Customer redesign (D-269): Home, search results, shop and product at phone and desktop width, on the
// e2e build with a faked API (no stack). Runs only with TZ_QA_DIR, e.g.
// `TZ_QA_DIR=docs/qa/customer-redesign/after pnpm exec playwright test e2e/redesign.shots.spec.ts`.
// Add TZ_AXE=<path to axe-core/axe.min.js> to fail on accessibility violations or sideways scroll.
import { shot } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the screenshots');

const SIZES = { phone: { width: 360, height: 780 }, desktop: { width: 1280, height: 860 } } as const;
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const NOW = new Date().toISOString();
const AGO = new Date(Date.now() - 12 * 60_000).toISOString();

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

// Trial photos (CC BY 2.0, design/trials/photos/CREDITS.md) stand in for shop and item photos in these shots only.
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

const SHOPS = [
  shop('sri-lakshmi-stores', 'Sri Lakshmi Provision Stores', { photoKey: 'shop-grocery.jpg', cashbackPct: 5 }),
  shop('jeevan-medicals', 'Jeevan Medicals', { photoKey: 'shop-pharmacy.jpg', categoryTint: 'rose', distanceM: 800, locality: 'HAL 2nd Stage' }),
  shop('mithai-mane', 'Mithai Mane Sweets', { photoKey: 'shop-sweets.jpg', categoryTint: 'amber', distanceM: 950, delivery: false, rating: 4.3, ratingCount: 48 }),
  shop('annapurna-kirana', 'Annapurna Kirana', { open: false, bookable: false, delivery: false, distanceM: 1200, rating: null, ratingCount: 0, categoryTint: 'peach' }),
];

const CHILDREN: Record<string, [string, string][]> = {
  grocery: [
    ['staples', 'Atta, rice and dal'],
    ['dairy-eggs', 'Dairy and eggs'],
    ['fruits-vegetables', 'Fruits and vegetables'],
    ['snacks', 'Snacks'],
    ['beverages', 'Beverages'],
    ['spices-oils', 'Spices and oils'],
    ['bakery', 'Bakery'],
    ['household', 'Household'],
    ['personal-care', 'Personal care'],
  ],
  pharmacy: [
    ['medicines', 'Medicines'],
    ['health-wellness', 'Health and wellness'],
    ['baby-care', 'Baby care'],
  ],
  beauty: [
    ['skin-care', 'Skin care'],
    ['hair-care', 'Hair care'],
    ['makeup', 'Makeup'],
  ],
  electronics: [
    ['mobile-accessories', 'Mobile accessories'],
    ['batteries-lighting', 'Batteries and lighting'],
    ['small-appliances', 'Small appliances'],
  ],
  stationery: [
    ['school-supplies', 'School supplies'],
    ['office-supplies', 'Office supplies'],
  ],
  hardware: [
    ['tools', 'Tools'],
    ['electrical', 'Electrical'],
    ['plumbing', 'Plumbing'],
    ['paints', 'Paints'],
  ],
};

const CATEGORIES = [
  ['grocery', 'Grocery', 'green', 'shopping-bag'],
  ['pharmacy', 'Pharmacy', 'rose', 'pill'],
  ['beauty', 'Beauty', 'lilac', 'sparkles'],
  ['electronics', 'Electronics', 'sky', 'smartphone'],
  ['stationery', 'Stationery', 'amber', 'pen'],
  ['hardware', 'Hardware', 'peach', 'wrench'],
].map(([slug = '', name, tint, icon], i) => ({
  id: `01KE0S1W6W8ZV7T1C3A6CAT00${i}`,
  slug,
  name,
  names: {},
  tint,
  icon,
  children: (CHILDREN[slug] ?? []).map(([child, childName], j) => ({
    id: `01KE0S1W6W8ZV7T1C3A6CA${i}${String(j).padStart(2, '0')}`,
    slug: child,
    name: childName,
    names: {},
    tint,
    icon,
    children: [],
  })),
}));

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
  confirmedAt: AGO,
  imageKey: null,
  imageCredit: null,
  ...over,
});

const ITEMS = [
  item('01', 'Nandini Toned Milk', { brand: 'Nandini', pricePaise: 2800, mrpPaise: 2800, imageKey: 'p-milk.jpg' }),
  item('02', 'Milk Bread', { brand: 'Modern', packSize: 400, unit: 'g', pricePaise: 4500, mrpPaise: 5000, hasOffer: true, imageKey: 'p-bread.jpg' }),
  item('05', 'Maggi Noodles', { brand: 'Maggi', packSize: 280, unit: 'g', pricePaise: 5600, mrpPaise: 5600, imageKey: 'p-noodles.jpg' }),
  item('06', 'Surf Excel Matic', { brand: 'Surf Excel', packSize: 1, unit: 'kg', pricePaise: 23000, mrpPaise: 25000, hasOffer: true, imageKey: 'p-wash.jpg' }),
  item('07', 'Dairy Milk Silk', { brand: 'Cadbury', packSize: 60, unit: 'g', pricePaise: 8000, mrpPaise: 9000, hasOffer: true, imageKey: 'p-choc.jpg' }),
  item('08', 'Paracetamol 500', { brand: null, packSize: 10, unit: 'pcs', pricePaise: 3200, mrpPaise: 3200, imageKey: 'p-tablets.jpg' }),
  item('09', 'USB-C cable 1 m', { brand: null, packSize: 1, unit: 'pcs', pricePaise: 19900, mrpPaise: 29900, hasOffer: true, imageKey: 'p-cable.jpg' }),
  item('10', 'Toor dal', { brand: 'Tata Sampann', packSize: 1, unit: 'kg', pricePaise: 16500, mrpPaise: 18000, hasOffer: true }),
  item('11', 'Amul Gold Milk', { brand: 'Amul', pricePaise: 3300, mrpPaise: 3300, inStock: false }),
];

const WEEK = Object.fromEntries(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, [{ open: '08:00', close: '22:00' }]]));
const storePage = (s: (typeof SHOPS)[number], categorySlug: string, categoryName: string) => ({
  ...s,
  addressLine1: null, addressLine2: null, landmark: null, city: 'Bengaluru', pincode: '560038', lat: 12.97, lng: 77.64,
  closedUntil: null, hours: { weekly: WEEK, holidays: [] }, categorySlug, categoryName,
  deliveryFeePaise: 2000, deliveryFreeAbovePaise: null, deliveryMinOrderPaise: 15000, deliveryRadiusM: 3000, serverNow: NOW,
});

const DISCOVER: Record<string, unknown> = {
  '/v1/discover/places/search': { items: [{ label: '560038', lat: 12.97, lng: 77.64 }] },
  '/v1/discover/summary': { storesNearby: 14, openNow: 9, productsInRadius: 1280, pins: [], pinsTruncated: false, serverNow: NOW },
  '/v1/discover/categories': { items: CATEGORIES },
  '/v1/discover/stores': { items: SHOPS, nextCursor: null },
  '/v1/discover/places/reverse': { name: 'Indiranagar', label: 'Indiranagar, Bengaluru 560038', pincode: '560038', city: 'Bengaluru' },
  '/v1/discover/offers': {
    items: [
      {
        kind: 'banner', storeId: SHOPS[0]?.storeId, storeSlug: 'sri-lakshmi-stores', storeName: 'Sri Lakshmi Provision Stores',
        locality: 'Indiranagar', tint: 'green', distanceM: 450, offer: null,
        banner: { headline: 'Fresh fruit, 10% off till 8 pm', imageKey: 'cat-fruit.jpg', template: null },
      },
      {
        kind: 'offer', storeId: SHOPS[1]?.storeId, storeSlug: 'jeevan-medicals', storeName: 'Jeevan Medicals',
        locality: 'HAL 2nd Stage', tint: 'rose', distanceM: 800, offer: { title: 'Flat ₹50 off on ₹499' }, banner: null,
      },
    ],
  },
  '/v1/discover/search': {
    query: 'milk',
    summary: { storesChecked: 12, storesWithIt: 3, minPricePaise: 2800 },
    results: [
      { store: SHOPS[0], items: [ITEMS[0], ITEMS[1]], badge: 'lowest_price', deliveryFeePaise: 2000 },
      { store: SHOPS[1], items: [ITEMS[4]], badge: 'closest', deliveryFeePaise: 0 },
      { store: SHOPS[3], items: [ITEMS[8]], badge: null, deliveryFeePaise: 0 },
    ],
    chips: [],
    refineHint: null,
    nextCursor: null,
    serverNow: NOW,
  },
  '/v1/discover/search/suggest': { items: [] },
  '/v1/discover/stores/sri-lakshmi-stores': storePage(SHOPS[0] as (typeof SHOPS)[number], 'grocery', 'Grocery'),
  '/v1/discover/stores/sri-lakshmi-stores/items': { items: ITEMS, nextCursor: null, serverNow: NOW },
  '/v1/discover/stores/jeevan-medicals': storePage(SHOPS[1] as (typeof SHOPS)[number], 'pharmacy', 'Pharmacy'),
  '/v1/discover/stores/jeevan-medicals/items': { items: [ITEMS[5], ITEMS[6], ITEMS[8]], nextCursor: null, serverNow: NOW },
  '/v1/discover/stores/mithai-mane/items': { items: [ITEMS[4], ITEMS[1]], nextCursor: null, serverNow: NOW },
};

/** With TZ_AXE (a path to axe.min.js), the screen must have no WCAG 2.2 AA violations. */
async function axe(page: Page, width: number) {
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
  expect(wide).toBeLessThanOrEqual(width);
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
    if (p === '/v1/me/addresses') return ok(route, { addresses: [] });
    if (p === '/v1/me/wallet') return ok(route, { availableCredits: 240 });
    if (p in DISCOVER) return ok(route, DISCOVER[p]);
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    return route.fulfill({ status: 404, headers: cors(route), body: '' });
  });
  await page.route('https://img.todayz.localhost/**', (route) => {
    const name = new URL(route.request().url()).pathname.split('/').pop() ?? '';
    try {
      return route.fulfill({ status: 200, contentType: 'image/jpeg', body: readFileSync(PHOTOS + name) });
    } catch {
      return route.fulfill({ status: 404, body: '' });
    }
  });
}

async function settle(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => [...document.images].every((i) => i.complete));
}

for (const [size, viewport] of Object.entries(SIZES)) {
  test.describe(size, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize(viewport);
      await signedIn(page);
    });

    test('home', async ({ page }) => {
      await page.goto('/');
      await page.getByText('Annapurna Kirana').first().waitFor();
      await page.getByText('Fresh fruit, 10% off till 8 pm').waitFor();
      await settle(page);
      await axe(page, viewport.width);
      await shot(page, `home-${size}.png`);
      await shot(page, `home-${size}-full.png`, { fullPage: true });
    });

    test('search results', async ({ page }) => {
      await page.goto('/search?q=milk');
      await page.getByText('Amul Gold Milk').waitFor();
      await settle(page);
      await axe(page, viewport.width);
      await shot(page, `search-${size}.png`);
    });

    test('shop with the cart bar', async ({ page }) => {
      await page.goto('/s/sri-lakshmi-stores');
      await page.getByText('Surf Excel Matic').waitFor();
      await settle(page);
      await axe(page, viewport.width);
      await shot(page, `shop-${size}.png`);
      await page.getByRole('button', { name: 'Add, Maggi Noodles', exact: true }).click();
      await page.getByRole('button', { name: 'Add, Milk Bread', exact: true }).click();
      await page.getByRole('button', { name: 'Add one, Milk Bread', exact: true }).click();
      await page.getByText('Sri Lakshmi Provision Stores').last().waitFor();
      await settle(page);
      await shot(page, `shop-${size}-cart.png`);
      await shot(page, `shop-${size}-full.png`, { fullPage: true });
    });

    test('product', async ({ page }) => {
      await page.goto('/s/sri-lakshmi-stores/i/01KE0S1W6W8ZV7T1C3A6ITEM02');
      await page.getByRole('heading', { name: 'Milk Bread' }).waitFor();
      await settle(page);
      await axe(page, viewport.width);
      await shot(page, `product-${size}.png`);
    });
  });
}
