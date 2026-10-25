import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-06 Voice update on the production-shaped build with a faked API (no stack, no model): hold to
// talk, processing, the draft with a cashier's "Ask manager" and "Pick item" lines, the edit sheet,
// Confirm and the off state. The microphone is a test tone (an oscillator), so the real
// MediaRecorder runs. `TZ_QA_DIR=docs/qa/W3-04 pnpm exec playwright test voice.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const DRAFT = '01J9ZDDDDDDDDDDDDDDDDDDDDD';
const NOW = () => new Date().toISOString();
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;
const voice = `${base}/ai/voice-stock`;

const id = (p: string, n: number) => `01J9Z${p.repeat(19)}${String(n).padStart(2, '0')}`;

function item(n: number, name: string, packSize: number, unit: string, pricePaise: number, onHand: number) {
  return {
    id: id('I', n),
    storeId: SID,
    productId: id('P', n),
    name,
    productName: name,
    localName: null,
    brand: null,
    variant: null,
    packSize,
    unit,
    categoryId: '01J9ZCCCCCCCCCCCCCCCCCCCCC',
    barcodes: [],
    pricePaise,
    mrpPaise: null,
    productMrpPaise: pricePaise + 200,
    onHand,
    reserved: 0,
    available: onHand,
    lowStockAt: null,
    lowStock: false,
    isListed: true,
    stockUpdatedAt: NOW(),
    stale: false,
    createdAt: NOW(),
    updatedAt: NOW(),
  };
}

const ITEMS = [
  item(1, 'Maggi noodles', 70, 'g', 1400, 36),
  item(2, 'Amul Taaza milk', 500, 'ml', 2800, 12),
  item(3, 'Parle-G biscuits', 100, 'g', 1000, 4),
  item(4, 'Surf Excel', 1, 'kg', 13000, 6),
  item(5, 'Kurkure masala munch', 90, 'g', 2000, 18),
];

function line(n: number, kind: string, over: Record<string, unknown>) {
  return { lineId: `l${n}`, kind, storeItemId: null, itemName: null, heard: '', quantity: null, pricePaise: null, currentPricePaise: null, onHand: null, confidence: 0.9, matched: true, disabled: null, ...over };
}

/** What a cashier gets back for "sold 2 Maggi, 10 Amul milk came in, Parle-G is out of stock, Surf Excel price 128, 3 Kurkure". */
const LINES = [
  line(1, 'sale', { storeItemId: id('I', 1), itemName: 'Maggi noodles · 70 g', heard: 'sold 2 Maggi', quantity: 2, currentPricePaise: 1400, onHand: 36, confidence: 0.94 }),
  line(2, 'restock', { storeItemId: id('I', 2), itemName: 'Amul Taaza milk · 500 ml', heard: '10 Amul milk came in', quantity: 10, currentPricePaise: 2800, onHand: 12, confidence: 0.91, disabled: 'ask_manager' }),
  line(3, 'count', { storeItemId: id('I', 3), itemName: 'Parle-G biscuits · 100 g', heard: 'Parle-G is out of stock', quantity: 0, currentPricePaise: 1000, onHand: 4, confidence: 0.88 }),
  line(4, 'set_price', { storeItemId: id('I', 4), itemName: 'Surf Excel · 1 kg', heard: 'Surf Excel price 128', pricePaise: 12800, currentPricePaise: 13000, onHand: 6, confidence: 0.86, disabled: 'ask_manager' }),
  line(5, 'sale', { heard: '3 kurkure', quantity: 3, confidence: 0.45, matched: false, itemName: 'kurkure' }),
];

/** The server reads the caller's role: only a cashier gets `ask_manager` on the restock and the price. */
function draft(status: string, role: string) {
  return {
    draftId: DRAFT,
    feature: 'voice_stock',
    status,
    promptVersion: 'voice_stock.v1',
    createdAt: NOW(),
    language: status === 'processing' ? null : 'en-IN',
    transcript: status === 'processing' ? null : 'sold 2 Maggi, 10 Amul milk came in, Parle-G is out of stock, Surf Excel price 128, 3 kurkure',
    lines: status === 'processing' ? [] : LINES.map((l) => (role === 'cashier' ? l : { ...l, disabled: null })),
    outcomes: [],
    failure: null,
  };
}

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

interface Fake {
  /** The draft turns ready on the next read once this is true. */
  ready: boolean;
  /** Voice is on for the shop. */
  on: boolean;
  confirm: unknown;
  uploads: number;
  idempotencyKey: string | undefined;
}

async function fakeApi(page: Page, role: string): Promise<Fake> {
  const fake: Fake = { ready: false, on: true, confirm: undefined, uploads: 0, idempotencyKey: undefined };
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role, stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (url.pathname === '/v1/discover/categories') return ok(route, { items: [], nextCursor: null });
    if (url.pathname === `${base}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: NOW() });
    if (url.pathname === `${base}/items`) {
      const q = (url.searchParams.get('q') ?? '').toLowerCase();
      return ok(route, { items: ITEMS.filter((i) => i.name.toLowerCase().includes(q)), nextCursor: null });
    }
    if (url.pathname === voice && fake.on) return ok(route, { maxSeconds: 15, maxBytes: 1_048_576, languages: ['en-IN', 'hi-IN', 'kn-IN', 'te-IN', 'ta-IN'] });
    if (url.pathname === `${voice}/drafts` && req.method() === 'POST') {
      fake.uploads++;
      return ok(route, { draftId: DRAFT, status: 'processing' }, 202);
    }
    if (url.pathname === `${voice}/drafts/${DRAFT}`) return ok(route, draft(fake.ready ? 'ready' : 'processing', role));
    if (url.pathname === `${voice}/drafts/${DRAFT}/confirm`) {
      fake.confirm = req.postDataJSON();
      fake.idempotencyKey = req.headers()['idempotency-key'];
      return ok(route, {
        draftId: DRAFT,
        status: 'confirmed',
        lines: [
          { lineId: 'l1', outcome: 'accepted' },
          { lineId: 'l2', outcome: 'not_permitted' },
          { lineId: 'l3', outcome: 'accepted' },
          { lineId: 'l4', outcome: 'not_permitted' },
          { lineId: 'l5', outcome: 'edited' },
        ],
      });
    }
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }) });
  });
  return fake;
}

/** A test tone for the microphone, so MediaRecorder and the waveform run without a device. */
async function fakeMicrophone(page: Page) {
  await page.addInitScript(() => {
    const media = navigator.mediaDevices as MediaDevices | undefined;
    if (!media) return;
    media.getUserMedia = () => {
      const ctx = new AudioContext();
      const osc = ctx.createOscillator();
      const wobble = ctx.createOscillator();
      const depth = ctx.createGain();
      wobble.frequency.value = 3;
      depth.gain.value = 0.6;
      wobble.connect(depth).connect(osc.frequency);
      osc.frequency.value = 220;
      const dest = ctx.createMediaStreamDestination();
      osc.connect(dest);
      osc.start();
      wobble.start();
      return Promise.resolve(dest.stream);
    };
  });
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
  await fakeMicrophone(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const noSideScroll = async (page: Page) => expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);

test('phone 360: a cashier speaks, checks the draft, picks an item and confirms (B-06)', async ({ page }) => {
  const fake = await fakeApi(page, 'cashier');
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/stock');
  await page.getByRole('link', { name: 'Voice update' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Voice update' })).toBeVisible();
  const talk = page.getByRole('button', { name: 'Hold to talk' });
  await expect(talk).toBeVisible();
  await noSideScroll(page);
  await shot(page, 'voice-idle-360.png', { fullPage: true });

  await talk.focus();
  await page.keyboard.down(' ');
  await expect(page.getByRole('button', { name: 'Let go to send' })).toBeVisible();
  await page.waitForTimeout(1500);
  await shot(page, 'voice-listening-360.png');
  await page.keyboard.up(' ');

  await expect(page.getByText('Working out what you said')).toBeVisible();
  expect(fake.uploads).toBe(1);
  await shot(page, 'voice-processing-360.png');
  fake.ready = true;

  const list = page.getByRole('list', { name: 'Draft lines' });
  await expect(list.getByRole('listitem')).toHaveCount(5);
  await expect(list.getByText('Ask manager')).toHaveCount(2);
  await expect(page.getByText('1 line needs an item or a number before you can confirm.')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Confirm/ })).toBeDisabled();
  await noSideScroll(page);
  await shot(page, 'voice-draft-360.png', { fullPage: true });

  await list.getByRole('button', { name: 'Pick item' }).click();
  const sheet = page.getByRole('dialog', { name: 'Pick the item' });
  await expect(sheet.getByRole('button', { name: 'Pick Kurkure masala munch · 90 g' })).toBeVisible();
  await shot(page, 'voice-pick-item-360.png');
  await sheet.getByRole('button', { name: 'Pick Kurkure masala munch · 90 g' }).click();
  await sheet.getByRole('button', { name: 'Save' }).click();
  await expect(sheet).toBeHidden();

  const confirm = page.getByRole('button', { name: 'Confirm 3 changes' });
  await expect(confirm).toBeEnabled();
  await shot(page, 'voice-ready-360.png', { fullPage: true });
  await confirm.click();
  await expect(page.getByText('Draft confirmed')).toBeVisible();
  await expect(page.getByText('3 changes applied, 2 left for a manager.')).toBeVisible();
  expect(fake.idempotencyKey).toBeTruthy();
  expect(fake.confirm).toEqual({
    lines: [
      { lineId: 'l1', outcome: 'accepted' },
      { lineId: 'l2', outcome: 'accepted' },
      { lineId: 'l3', outcome: 'accepted' },
      { lineId: 'l4', outcome: 'accepted' },
      { lineId: 'l5', outcome: 'edited', value: { kind: 'sale', storeItemId: id('I', 5), quantity: 3 } },
    ],
  });
  await noSideScroll(page);
  await shot(page, 'voice-done-360.png', { fullPage: true });
});

test('phone 360: editing a line, a manager sees no ask-manager flags (B-06)', async ({ page }) => {
  const fake = await fakeApi(page, 'manager');
  fake.ready = true;
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/voice');
  const talk = page.getByRole('button', { name: 'Hold to talk' });
  await talk.focus();
  await page.keyboard.down(' ');
  await expect(page.getByRole('button', { name: 'Let go to send' })).toBeVisible();
  await page.waitForTimeout(800);
  await page.keyboard.up(' ');
  const list = page.getByRole('list', { name: 'Draft lines' });
  await expect(list.getByRole('listitem')).toHaveCount(5, { timeout: 5000 });
  await expect(list.getByText('Ask manager')).toHaveCount(0);

  await list.getByRole('listitem').nth(3).getByRole('button', { name: 'Edit' }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit line' });
  await sheet.getByLabel('Selling price').fill('0');
  await sheet.getByRole('button', { name: 'Save' }).click();
  await expect(sheet.getByText('Enter a price above ₹0.')).toBeVisible();
  await noSideScroll(page);
  await shot(page, 'voice-edit-360.png');
});

test('phone 360: voice off hides the screen and the Stock link (B-06)', async ({ page }) => {
  const fake = await fakeApi(page, 'owner');
  fake.on = false;
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/stock');
  await expect(page.getByRole('heading', { level: 1, name: 'Stock' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Voice update' })).toHaveCount(0);
  await page.goto('/voice');
  await expect(page.getByText('Voice update is off')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Hold to talk' })).toHaveCount(0);
  await shot(page, 'voice-off-360.png');
  // The off answer is the API's 404, which the browser logs; allow exactly those and nothing else.
  const missing = guard.responses.filter((r) => r.status === 404).map((r) => new URL(r.url).pathname);
  expect(missing.length).toBeGreaterThan(0);
  expect(missing.every((p) => p === voice)).toBe(true);
  guard.errors.splice(0, guard.errors.length, ...guard.errors.filter((e) => !e.includes('status of 404')));
});

test('desktop: the draft beside the sidebar (B-06)', async ({ page }) => {
  const fake = await fakeApi(page, 'owner');
  fake.ready = true;
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/voice');
  const talk = page.getByRole('button', { name: 'Hold to talk' });
  await talk.focus();
  await page.keyboard.down(' ');
  await expect(page.getByRole('button', { name: 'Let go to send' })).toBeVisible();
  await page.waitForTimeout(800);
  await page.keyboard.up(' ');
  await expect(page.getByRole('list', { name: 'Draft lines' }).getByRole('listitem')).toHaveCount(5);
  await shot(page, 'voice-draft-desktop.png', { fullPage: true });
});
