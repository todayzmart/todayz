import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answer, answerWith, problem, signIn, type Answer } from './fake-api';

/**
 * W3-06 journeys against the built console on a phone, with the field API faked statefully:
 * A-04 takes one shop from nothing to the owner's code (SC-FA-01…05), and A-15 shows an agent their
 * own numbers while an admin sees every agent (SC-FA-08).
 */

const BID = '01JF1E1DBUS000000000000B01';
const SID = '01JF1E1DST0RE0000000000S01';
const CAT = '01JCATEG0RY0000000000000C1';
const CHILD = '01JCATEG0RYCH1LD00000000C2';
const AGENT = '01JADMIN0000000000000000AD';
const UPLOAD = 'https://s3.todayz.localhost/todayz-private/field/front.jpg';

/** A-04 is built for the smallest phone agents carry (360px wide), below the preset's phone. */
const PHONE = { ...VIEWPORTS.phone, width: 360 };

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(PHONE);
});

/** Nothing on the screen is wider than the phone, so the page never scrolls sideways. */
async function fitsTheScreen(page: Page) {
  const [scroll, width] = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth]);
  expect(scroll, 'page width on a phone').toBeLessThanOrEqual(width);
}

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

const categories = {
  items: [
    {
      id: CAT,
      slug: 'grocery',
      name: 'Grocery',
      names: {},
      tint: 'mint',
      icon: 'shopping-basket',
      children: [{ id: CHILD, slug: 'dal', name: 'Dals and pulses', names: {}, tint: 'mint', icon: 'bean' }],
    },
  ],
};

const products = [
  { id: '01JF1E1DPR0D00000000000P01', name: 'Toor dal', brand: 'Tata Sampann', variant: null, packSize: 1, unit: 'kg', mrpPaise: 18_000, categoryId: CHILD },
  { id: '01JF1E1DPR0D00000000000P02', name: 'Sona masoori rice', brand: 'India Gate', variant: null, packSize: 5, unit: 'kg', mrpPaise: 42_000, categoryId: CHILD },
];

/** Records each write's path and body, so a test can check what the page sent. */
async function fake(page: Page, path: string, reply: (method: string, url: URL, body: unknown) => Answer) {
  const sent: { method: string; path: string; body: unknown }[] = [];
  await answerWith(page, path, (req, url) => {
    const raw = req.postData();
    const body = raw && req.headers()['content-type']?.includes('json') ? (JSON.parse(raw) as unknown) : undefined;
    if (req.method() !== 'GET') sent.push({ method: req.method(), path: url.pathname, body });
    return reply(req.method(), url, body);
  });
  return sent;
}

/** The field API over one shop that starts empty, keeping what the screens change. */
async function fieldApi(page: Page) {
  type Item = { id: string; productId: string; name: string; pricePaise: number; onHand: number | null };
  let shop: Record<string, unknown> | null = null;
  let stores: Record<string, unknown>[] = [];
  let photos = 0;
  let items: Item[] = [];
  let tries = 0;
  const business = () => ({
    ...shop,
    stores: stores.map((s) => ({ ...s, itemCount: items.length })),
    photoCount: photos,
    updatedAt: NOW,
  });
  const fieldItem = (i: Item) => {
    const p = products.find((x) => x.id === i.productId);
    return {
      ...i,
      storeId: SID,
      brand: p?.brand ?? null,
      variant: null,
      packSize: p?.packSize ?? 1,
      unit: p?.unit ?? 'kg',
      mrpPaise: p?.mrpPaise ?? null,
      productMrpPaise: p?.mrpPaise ?? null,
      isListed: true,
      productStatus: 'active',
    };
  };
  await answer(page, '/v1/discover/categories', 200, categories);
  // The presigned PUT goes to object storage, not the API.
  const uploads: string[] = [];
  await answerWith(page, '/todayz-private/**', (req, url) => {
    uploads.push(`${req.method()} ${url.host}`);
    return { status: 200, body: null };
  });
  const base = `/v1/admin/field/businesses/${BID}`;
  const sent = await fake(page, '/v1/admin/field/businesses**', (method, url, body) => {
    const p = url.pathname;
    const b = body as Record<string, unknown>;
    if (p === '/v1/admin/field/businesses') {
      if (method === 'GET') return { status: 200, body: { items: shop ? [business()] : [], nextCursor: null, serverNow: NOW } };
      shop = {
        id: BID,
        brandName: b['brandName'],
        legalName: null,
        categoryId: b['categoryId'] ?? null,
        status: 'draft',
        ownerPhoneMasked: '+91 ••••••3210',
        ownerConfirmed: false,
        onboardedBy: AGENT,
        verifiedAt: null,
        editableUntil: null,
        itemsEditable: true,
        createdAt: NOW,
      };
      return { status: 201, body: business() };
    }
    if (!shop) return { status: 404, body: problem(404, 'not_found') };
    if (p === base) {
      if (method === 'PATCH') shop = { ...shop, ...b };
      return { status: 200, body: business() };
    }
    if (p === `${base}/stores` && method === 'POST') {
      stores = [{ id: SID, name: b['name'], status: 'draft', locality: b['locality'] }];
      return { status: 201, body: { ...stores[0], itemCount: 0 } };
    }
    if (p === `${base}/documents`) {
      photos += 1;
      return {
        status: 201,
        body: {
          document: { id: '01JD0C000000000000000000D9', kind: b['kind'], contentType: b['contentType'], storeId: SID, status: 'uploaded', createdAt: NOW },
          upload: { url: UPLOAD, method: 'PUT', headers: { 'Content-Type': b['contentType'] }, expiresAt: NOW },
        },
      };
    }
    if (p === `${base}/stores/${SID}/products`) {
      const q = (url.searchParams.get('q') ?? '').toLowerCase();
      const hits = products.filter((x) => x.name.toLowerCase().includes(q)).map((x) => ({ ...x, inStore: items.some((i) => i.productId === x.id) }));
      return { status: 200, body: { items: hits } };
    }
    if (p === `${base}/stores/${SID}/items`) {
      if (method === 'GET') return { status: 200, body: { items: items.map(fieldItem), nextCursor: null, itemsEditable: true, editableUntil: null, serverNow: NOW } };
      const productId = b['productId'] as string;
      if (items.some((i) => i.productId === productId)) return { status: 409, body: problem(409, 'conflict') };
      const added = {
        id: `01JF1E1D1TEM00000000000I0${items.length + 1}`,
        productId,
        name: products.find((x) => x.id === productId)?.name ?? '',
        pricePaise: b['pricePaise'] as number,
        onHand: (b['openingStock'] as number | undefined) ?? null,
      };
      items = [...items, added];
      return { status: 201, body: fieldItem(added) };
    }
    if (p === `${base}/owner-confirm`) {
      if (!shop['categoryId']) return { status: 409, body: { ...problem(409, 'submit_incomplete'), fields: { categoryId: 'required' } } };
      return { status: 200, body: { phoneMasked: '+91 ••••••3210', expiresAt: '2026-10-25T06:40:00Z', resendAfterSeconds: 30 } };
    }
    if (p === `${base}/owner-confirm/verify`) {
      tries += 1;
      if ((b['code'] as string) !== '482913')
        return { status: 422, body: { ...problem(422, 'code_invalid'), extensions: { attemptsLeft: 5 - tries } } };
      shop = { ...shop, status: 'submitted', ownerConfirmed: true };
      return { status: 200, body: business() };
    }
    return undefined;
  });
  return { sent, uploads };
}

/** The wizard shows this step: its tab is the current one. */
async function onStep(page: Page, name: string) {
  const steps = page.getByRole('list', { name: 'Steps' });
  await expect(steps.getByRole('button', { name: new RegExp(name) })).toHaveAttribute('aria-current', 'step');
}

async function typeCode(page: Page, code: string) {
  await page.getByRole('group', { name: "Owner's code" }).getByRole('textbox', { name: "Owner's code" }).click();
  await page.keyboard.type(code);
}

test('A-04: a field agent adds a shop on a phone, from the name to the owner confirming', async ({ page }) => {
  await signIn(page, ['field_agent']);
  const { sent, uploads } = await fieldApi(page);
  const main = page.getByRole('main');

  await page.goto('/');
  await expect(page).toHaveURL(/\/field\/?$/);
  await expect(main.getByRole('heading', { name: 'No shops yet' })).toBeVisible();
  await main.getByRole('button', { name: 'Add a shop' }).first().click();

  // SC-FA-01: the owner's number is checked on the phone before anything is created.
  await main.getByLabel('Shop name').fill('Ganesh Kirana');
  await main.getByLabel(/Owner's mobile number/).fill('12345');
  await main.getByRole('button', { name: 'Create shop' }).click();
  await expect(main.getByText('Enter a 10-digit Indian mobile number.')).toBeVisible();
  expect(sent).toEqual([]);
  await main.getByLabel(/Owner's mobile number/).fill('98765 43210');
  await fitsTheScreen(page);
  await shot(page, 'admin-a04-new.png');
  await main.getByRole('button', { name: 'Create shop' }).click();
  await expect(page).toHaveURL(new RegExp(`/field/${BID}$`));
  expect(sent[0]).toMatchObject({ method: 'POST', body: { brandName: 'Ganesh Kirana', ownerPhone: '+919876543210' } });

  // No category yet, so the wizard opens on Details; the owner's code is refused until it is set.
  await onStep(page, 'Details');
  await main.getByRole('button', { name: /Owner/ }).click();
  await main.getByRole('button', { name: 'Send code to owner' }).click();
  await expect(main.getByText('Some details are missing before the owner can confirm.')).toBeVisible();
  await main.getByRole('button', { name: 'Add the category' }).click();
  await onStep(page, 'Details');
  await main.getByLabel('Category').selectOption({ label: 'Grocery' });
  await main.getByRole('button', { name: 'Save and continue' }).click();

  // Store: address, pin, phone and hours.
  const store = main.getByRole('form', { name: 'Store details' });
  await expect(store).toBeVisible();
  await store.getByLabel('Store name').fill('Ganesh Kirana, Basavanagudi');
  await store.getByLabel('Address', { exact: true }).fill('12, 3rd Cross, Gandhi Bazaar');
  await store.getByLabel('Area').fill('Basavanagudi');
  await store.getByLabel('City').fill('Bengaluru');
  await store.getByLabel('PIN code').fill('560004');
  await store.getByLabel('Latitude').fill('12.942100');
  await store.getByLabel('Longitude').fill('77.573600');
  await store.getByLabel('Store phone').fill('98765 43211');
  await fitsTheScreen(page);
  await shot(page, 'admin-a04-store.png', { fullPage: true });
  await store.getByRole('button', { name: 'Save store' }).click();
  await onStep(page, 'Photos');
  const storePost = sent.find((s) => s.path.endsWith('/stores'));
  expect(storePost?.body).toMatchObject({ pincode: '560004', phone: '+919876543211', location: { lat: 12.9421, lng: 77.5736 } });
  expect((storePost?.body as { hours: { weekly: Record<string, unknown[]> } }).hours.weekly['sun']).toEqual([]);

  // Photos: the file goes to the presigned URL, never through the API.
  await main.getByTestId('field-photo-input').setInputFiles({ name: 'front.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('jpeg') });
  await expect(page.getByRole('status').getByText('Photo added')).toBeVisible();
  expect(uploads).toEqual(['PUT s3.todayz.localhost']);
  await expect(main.getByText('1 photo added.')).toBeVisible();
  await main.getByRole('button', { name: 'Next' }).click();

  // SC-FA-04: products one by one, from the catalogue search.
  await onStep(page, 'Products');
  await main.getByLabel('Find a product').fill('dal');
  await main.getByRole('button', { name: 'Add Toor dal' }).click();
  const add = main.getByRole('form', { name: 'Add Toor dal' });
  await add.getByLabel('Price').fill('175');
  await add.getByLabel('Opening stock').fill('12');
  await add.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByRole('status').getByText('Toor dal added')).toBeVisible();
  await expect(main.getByRole('list', { name: 'Products in this store' }).getByText('Toor dal')).toBeVisible();
  expect(sent.find((s) => s.path.endsWith('/items'))?.body).toEqual({ productId: products[0]?.id, pricePaise: 17_500, openingStock: 12 });
  // The search clears after an add; searching again shows the product as already listed.
  await main.getByLabel('Find a product').fill('dal');
  await expect(main.getByRole('list', { name: 'Matching products' }).getByText('Already listed')).toBeVisible();
  await fitsTheScreen(page);
  await shot(page, 'admin-a04-items.png', { fullPage: true });
  await main.getByRole('button', { name: 'Next' }).click();

  // SC-FA-02/03: the owner reads out the code; a wrong one says how many tries are left.
  await main.getByRole('button', { name: 'Send code to owner' }).click();
  await expect(main.getByText(/Code sent to \+91 ••••••3210/)).toBeVisible();
  await expect(main.getByRole('button', { name: /Send a new code in \d+ seconds/ })).toBeDisabled();
  await typeCode(page, '111111');
  await expect(main.getByText('That code is not right. 4 tries left.')).toBeVisible();
  await fitsTheScreen(page);
  await shot(page, 'admin-a04-owner.png');
  await typeCode(page, '482913');
  await expect(main.getByText('The owner has confirmed')).toBeVisible();
  await expect(main.getByText('Waiting for review')).toBeVisible();
  await shot(page, 'admin-a04-confirmed.png');

  // The code never appears in a URL.
  expect(sent.filter((s) => s.path.includes('482913'))).toEqual([]);

  await main.getByRole('link', { name: 'Back to your shops' }).click();
  const list = main.getByRole('list', { name: 'Your shops' });
  await expect(list.getByRole('link', { name: /Ganesh Kirana/ })).toBeVisible();
  await fitsTheScreen(page);
  await shot(page, 'admin-a04-list.png');
  expect(guard.errors.filter((e) => !e.includes('409') && !e.includes('422')), 'console and page errors').toEqual([]);
});

const week = (i: number) => ({
  weekStart: new Date(Date.UTC(2026, 7, 31 + i * 7)).toISOString().slice(0, 10),
  onboarded: [1, 3, 2, 4, 0, 2, 5, 3][i] ?? 0,
  verified: [0, 2, 1, 3, 0, 1, 4, 2][i] ?? 0,
  itemsAdded: [12, 40, 25, 61, 0, 30, 88, 44][i] ?? 0,
});

const agent = (agentId: string, name: string, total: number) => ({
  agentId,
  name,
  businesses: { total, draft: 1, submitted: 1, needsInfo: 0, verified: total - 2, rejected: 0, suspended: 0 },
  storesLive: total - 3,
  itemsAdded: total * 14,
  inWindow: 2,
  firstOrderRate: 0.4,
  weekly: Array.from({ length: 8 }, (_, i) => week(i)),
});

test('A-15: a field agent sees only their own numbers', async ({ page }) => {
  await signIn(page, ['field_agent']);
  await answer(page, '/v1/admin/field/me/stats', 200, { stats: agent(AGENT, 'Asha Rao', 20), serverNow: NOW });
  const asked: string[] = [];
  page.on('request', (r) => asked.push(new URL(r.url()).pathname));

  await page.goto('/field-agents');
  const main = page.getByRole('main');
  const tiles = main.getByRole('list', { name: 'Your numbers' });
  await expect(tiles.getByRole('listitem')).toHaveCount(6);
  await expect(tiles).toContainText('20');
  await expect(main.getByRole('list', { name: 'Shops onboarded per week' }).getByRole('listitem')).toHaveCount(8);
  expect(asked.some((p) => p.startsWith('/v1/admin/field-agents'))).toBe(false);
  await fitsTheScreen(page);
  await shot(page, 'admin-a15-mine.png', { fullPage: true });
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('A-15: an admin sees every agent and picks whose weekly trend to show', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await signIn(page, ['admin']);
  await answer(page, '/v1/admin/field-agents**', 200, {
    items: [agent('01JAGENT00000000000000A001', 'Suresh', 18), agent('01JAGENT00000000000000A002', 'Lakshmi', 11)],
    nextCursor: null,
    serverNow: NOW,
  });

  await page.goto('/field-agents');
  const main = page.getByRole('main');
  const table = main.getByRole('table', { name: 'Field agents' });
  await expect(table.getByRole('row')).toHaveCount(3);
  await expect(main.getByRole('heading', { name: 'Weekly trend: Suresh' })).toBeVisible();
  await table.getByRole('button', { name: 'Show the weekly trend of Lakshmi' }).click();
  await expect(main.getByRole('heading', { name: 'Weekly trend: Lakshmi' })).toBeVisible();
  await shot(page, 'admin-a15-all.png', { fullPage: true });
  expect(guard.errors, 'console and page errors').toEqual([]);
});
