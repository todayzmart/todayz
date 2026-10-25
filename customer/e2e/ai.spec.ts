import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// W3-12 consumer AI at 360 px on the e2e build with a faked API (no stack, no model): C-23 voice
// search (the transcript fills the box and never searches; the search then says source=voice; the
// cap disables the mic), C-05 search assist chips that apply only when tapped, and C-22 list parse
// (draft lines reviewed one by one; only the kept lines join the basket). The microphone is a
// synthetic tone recorded by the browser's own MediaRecorder.
// `TZ_QA_DIR=docs/qa/W3-12 pnpm exec playwright test ai.spec.ts` refreshes the committed shots.

const PHONE = { width: 360, height: 780 } as const;
const NOW = '2026-10-23T10:00:00.000Z';
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const BASKET_ID = '01KE0MK0000000000000000001';
const DRAFT_ID = '01KE0LP0000000000000000001';
const STORE_ID = '01KE0S1W6W8ZV7T1C3A6MS0001';
const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/;

const store = {
  storeId: STORE_ID,
  storeSlug: 'sri-lakshmi-stores',
  name: 'Sri Lakshmi Stores',
  locality: 'Indiranagar',
  distanceM: 450,
  open: true,
  pickup: true,
  delivery: true,
  bookable: true,
  rating: 4.6,
  ratingCount: 38,
  cashbackPct: 0,
  categoryTint: null,
};
const item = {
  storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM01',
  productId: '01KE0S1W6W8ZV7T1C3A6PROD01',
  name: 'Aashirvaad Atta',
  variant: null,
  packSize: 5,
  unit: 'kg',
  pricePaise: 24000,
  mrpPaise: 26000,
  inStock: true,
  qtyAvailable: 12,
  imageKey: null,
  confirmedAt: '2026-10-23T09:48:00.000Z',
  offer: null,
};
const searchResult = (q: string, refineHint: boolean) => ({
  query: q,
  summary: { storesChecked: 12, storesWithIt: 1, minPricePaise: 24000 },
  results: [{ store, items: [item], badge: 'lowest_price', deliveryFeePaise: 2000 }],
  chips: [],
  refineHint,
  nextCursor: null,
  serverNow: NOW,
});

const draftLines = [
  { lineId: 'l1', text: 'atta 1 kg', quantity: 1 },
  { lineId: 'l2', text: 'nandini milk', quantity: 2 },
  { lineId: 'l3', text: 'dolo 650', quantity: 1 },
];
const draft = (status: 'processing' | 'ready') => ({
  draftId: DRAFT_ID,
  feature: 'list_parse',
  status,
  promptVersion: 'list_parse@1',
  createdAt: NOW,
  lines: status === 'ready' ? draftLines : [],
  outcomes: [],
  failure: null,
});

interface Seen {
  method: string;
  path: string;
  query: URLSearchParams;
  body: unknown;
  contentType: string;
  key: string | undefined;
}
interface Answers {
  voice?: 'ok' | 'cap';
}

async function fakeApi(page: Page, answers: Answers = {}): Promise<Seen[]> {
  const seen: Seen[] = [];
  let polls = 0;
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
    const send = (status: number, body?: unknown) =>
      route.fulfill({
        status,
        contentType: status >= 400 ? 'application/problem+json' : 'application/json',
        headers: cors,
        body: body === undefined ? '' : JSON.stringify(body),
      });
    const problem = (status: number, code: string) => send(status, { type: `about:blank#${code}`, title: code, status, code });
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors });
    const p = url.pathname;
    const contentType = h['content-type'] ?? '';
    seen.push({
      method: req.method(),
      path: p,
      query: url.searchParams,
      body: contentType.startsWith('application/json') ? (req.postDataJSON() as unknown) : undefined,
      contentType,
      key: h['idempotency-key'],
    });
    if (p === '/v1/auth/refresh')
      return send(200, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: USER });
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors, body: '' });
    if (p === '/v1/me/ai/features')
      return send(200, {
        listParse: true,
        voiceSearch: true,
        searchAssist: true,
        maxAudioSeconds: 15,
        maxAudioBytes: 1_000_000,
        audioTypes: ['audio/webm', 'audio/ogg', 'audio/mp4'],
      });
    if (p === '/v1/me/ai/voice-search')
      return answers.voice === 'cap' ? problem(429, 'ai_daily_cap_reached') : send(200, { transcript: 'do kilo atta', language: 'hi', chips: [] });
    if (p === '/v1/me/ai/search-assist')
      return send(200, {
        query: 'atta',
        chips: [
          { kind: 'maxPrice', value: '30000', text: 'under 300' },
          { kind: 'openNow', value: null, text: 'open now' },
        ],
      });
    if (p === '/v1/discover/search') {
      const q = url.searchParams.get('q') ?? '';
      return send(200, searchResult(q, q.includes('under')));
    }
    if (p === '/v1/discover/search/suggest') return send(200, { items: [] });
    if (p === '/v1/discover/categories') return send(200, { items: [] });
    if (p === `/v1/me/baskets/${BASKET_ID}`)
      return send(200, { basketId: BASKET_ID, name: 'Weekly', lines: [], lastSplit: null, updatedAt: NOW, version: 1 });
    if (p === '/v1/me/ai/list-parse/drafts' && req.method() === 'POST') return send(202, { draftId: DRAFT_ID });
    if (p === `/v1/me/ai/list-parse/drafts/${DRAFT_ID}`) {
      polls += 1;
      return send(200, draft(polls === 1 ? 'processing' : 'ready'));
    }
    if (p === `/v1/me/ai/list-parse/drafts/${DRAFT_ID}/confirm`)
      return send(200, {
        draftId: DRAFT_ID,
        status: 'confirmed',
        lines: draftLines,
        basketLines: [
          { lineId: 'l1', text: 'atta 1 kg', quantity: 1 },
          { lineId: 'l2', text: 'nandini milk 500 ml', quantity: 2 },
        ],
      });
    return problem(404, 'not_found');
  });
  return seen;
}

let guard: PageGuard;
// The browser logs every 4xx it receives; a test that fakes one on purpose lists its status here.
let allowed: number[] = [];

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  allowed = [];
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => {
    localStorage.setItem('todayz.lang', 'en');
    localStorage.setItem('todayz.hasSession.customer', '1');
    localStorage.setItem('todayz.place', JSON.stringify({ kind: 'gps', lat: 12.97, lng: 77.64 }));
    // A microphone that plays a tone, so the browser's MediaRecorder makes a real webm/opus file.
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: async () => {
          const ctx = new AudioContext();
          const osc = ctx.createOscillator();
          const out = ctx.createMediaStreamDestination();
          osc.connect(out);
          osc.start();
          return out.stream;
        },
      },
    });
  });
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  const errors = guard.errors.filter((e) => !allowed.some((s) => e.includes(`status of ${s}`)));
  expect(errors, 'console and page errors').toEqual([]);
});

test('360: C-23 the transcript fills the box, never searches, then searches with source=voice', async ({ page }) => {
  const seen = await fakeApi(page);
  await page.goto('/search');
  const mic = page.getByRole('button', { name: 'Search by voice' });
  await expect(mic).toBeVisible();
  await shot(page, 'customer-c23-mic-360.png');

  await mic.click();
  const stop = page.getByRole('button', { name: 'Stop and use what I said' });
  await expect(stop).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByText(/^Listening… \d+ s left/)).toBeVisible();
  await page.waitForTimeout(1200);
  await shot(page, 'customer-c23-listening-360.png');
  await stop.click();

  const box = page.getByRole('searchbox');
  await expect(box).toHaveValue('do kilo atta');
  await expect(page.getByText('Check the words, then search.')).toBeVisible();
  const upload = seen.find((s) => s.path === '/v1/me/ai/voice-search');
  expect(upload?.contentType).toMatch(/^multipart\/form-data/);
  expect(seen.some((s) => s.path === '/v1/discover/search')).toBe(false);
  await shot(page, 'customer-c23-transcript-360.png');

  await box.press('Enter');
  await expect(page.getByText('Sri Lakshmi Stores')).toBeVisible();
  const search = seen.find((s) => s.path === '/v1/discover/search');
  expect(search?.query.get('q')).toBe('do kilo atta');
  expect(search?.query.get('source')).toBe('voice');
});

test('360: C-23 at the daily cap the mic is disabled with the copy', async ({ page }) => {
  allowed = [429];
  await fakeApi(page, { voice: 'cap' });
  await page.goto('/search');
  await page.getByRole('button', { name: 'Search by voice' }).click();
  await page.waitForTimeout(800);
  await page.getByRole('button', { name: 'Stop and use what I said' }).click();
  await expect(page.getByRole('button', { name: 'Voice search is used up for today. Type instead.' })).toBeDisabled();
  await expect(page.getByText('Voice search is used up for today. Type instead.', { exact: true })).toBeVisible();
  await shot(page, 'customer-c23-capped-360.png');
});

test('360: C-05 search assist chips after the results apply only when tapped', async ({ page }) => {
  const seen = await fakeApi(page);
  await page.goto('/search?q=aata%20under%20300%20open');
  await expect(page.getByText('Sri Lakshmi Stores')).toBeVisible();
  const group = page.getByRole('group', { name: 'Suggestions for this search' });
  await expect(group.getByRole('button', { name: 'Search for “atta”' })).toBeVisible();
  const before = seen.filter((s) => s.path === '/v1/discover/search').length;
  await shot(page, 'customer-c05-assist-360.png');

  await group.getByRole('button', { name: /Under ₹300/ }).click();
  await expect(page.getByRole('group', { name: 'Added filters' }).getByRole('button', { name: /Under ₹300/ })).toBeVisible();
  const searches = seen.filter((s) => s.path === '/v1/discover/search');
  expect(searches.length).toBeGreaterThan(before);
  expect(searches.at(-1)?.query.get('maxPricePaise')).toBe('30000');
  await expect(page.getByText('Sri Lakshmi Stores')).toBeVisible();
  await expect(group.getByRole('button', { name: /Under ₹300/ })).toHaveCount(0);
  await expect(group.getByRole('button', { name: 'Open now' })).toBeVisible();
  await shot(page, 'customer-c05-assist-applied-360.png');
});

test('360: C-22 a pasted list becomes draft lines; only the kept ones join the basket', async ({ page }) => {
  const seen = await fakeApi(page);
  await page.goto(`/baskets/${BASKET_ID}`);
  await page.getByRole('button', { name: 'Paste a list' }).click();
  await page.getByLabel('Your list').fill('atta 1 kg, 2 nandini milk, dolo 650');
  await shot(page, 'customer-c22-list-360.png', { fullPage: true });
  await page.getByRole('button', { name: 'Read my list' }).click();

  const review = page.getByRole('region', { name: 'Check your list' });
  await expect(review).toBeVisible();
  const create = seen.find((s) => s.path === '/v1/me/ai/list-parse/drafts');
  expect(create?.body).toEqual({ text: 'atta 1 kg, 2 nandini milk, dolo 650' });
  expect(create?.key).toMatch(ULID);
  await review.getByRole('textbox', { name: 'Item 2' }).fill('nandini milk 500 ml');
  await review.getByRole('button', { name: 'Remove dolo 650' }).click();
  await shot(page, 'customer-c22-list-review-360.png', { fullPage: true });

  await review.getByRole('button', { name: 'Add 2 items to basket' }).click();
  const list = page.getByRole('list', { name: 'Items in this basket' });
  await expect(list.getByText('nandini milk 500 ml')).toBeVisible();
  await expect(list.getByText('dolo 650')).toHaveCount(0);
  const confirm = seen.find((s) => s.path.endsWith('/confirm'));
  expect(confirm?.body).toEqual({
    lines: [
      { lineId: 'l1', outcome: 'accepted' },
      { lineId: 'l2', outcome: 'edited', value: { text: 'nandini milk 500 ml', quantity: 2 } },
      { lineId: 'l3', outcome: 'discarded' },
    ],
  });
  await shot(page, 'customer-c22-list-added-360.png', { fullPage: true });
});
