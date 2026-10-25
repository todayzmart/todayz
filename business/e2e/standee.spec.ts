import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-10 standee campaigns (W3-09) on the production-shaped build with a faked API (no stack): the
// funnel per standee and in total, adding a standee and asking for a reprint.
// `TZ_QA_DIR=docs/qa/W3-09 pnpm exec playwright test standee.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;

interface Counts {
  scans: number;
  signUps: number;
  qualified: number;
}

function standee(n: number, campaign: string, counts: Counts, lastScanAt: string | null, reorderRequestedAt: string | null = null) {
  const id = `01J9ZQQQQQQQQQQQQQQQQQQQ${String(n).padStart(2, '0')}`;
  return { id, campaign, link: `https://api.todayz.localhost/v1/public/ref/K7Q2X9?s=${id}`, counts, lastScanAt, reorderRequestedAt, printedAt: null, createdAt: '2026-10-18T10:00:00Z', version: 1 };
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

async function fakeApi(page: Page) {
  let items = [
    standee(3, 'Diwali gate', { scans: 46, signUps: 11, qualified: 4 }, '2026-10-24T18:20:00Z'),
    standee(2, 'Counter', { scans: 128, signUps: 32, qualified: 19 }, '2026-10-25T08:05:00Z'),
    standee(1, 'Billing desk', { scans: 0, signUps: 0, qualified: 0 }, null, '2026-10-22T11:00:00Z'),
  ];
  const list = () => ({
    items,
    totals: items.reduce((t, s) => ({ scans: t.scans + s.counts.scans, signUps: t.signUps + s.counts.signUps, qualified: t.qualified + s.counts.qualified }), { scans: 0, signUps: 0, qualified: 0 }),
    limit: 20,
  });
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'verified', role: 'owner', stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (url.pathname === base && req.method() === 'GET') return ok(route, { id: SID, shortCode: 'K7Q2X9' });
    if (url.pathname === `${base}/standees` && req.method() === 'GET') return ok(route, list());
    if (url.pathname === `${base}/standees` && req.method() === 'POST') {
      const { campaign } = req.postDataJSON() as { campaign: string };
      const made = standee(10 + items.length, campaign, { scans: 0, signUps: 0, qualified: 0 }, null);
      items = [made, ...items];
      return ok(route, made, 201);
    }
    const reorder = /\/standees\/([^/]+)\/reorder$/.exec(url.pathname);
    if (reorder && req.method() === 'POST') {
      items = items.map((s) => (s.id === reorder[1] ? { ...s, reorderRequestedAt: new Date().toISOString(), version: s.version + 1 } : s));
      return ok(route, items.find((s) => s.id === reorder[1]));
    }
    if (url.pathname === `${base}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: new Date().toISOString() });
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
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const campaigns = (page: Page) => page.locator('section', { has: page.getByRole('heading', { name: 'Campaigns' }) });

test('desktop: standee campaigns with the funnel, a new standee and a reprint (B-10, W3-09)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/standee');
  const card = campaigns(page);
  const totals = card.getByLabel('All standees');
  await expect(totals.getByText('174')).toBeVisible();
  await expect(totals.getByText('43')).toBeVisible();
  await expect(totals.getByText('23')).toBeVisible();
  await expect(card.getByLabel('Counter counts').getByText('128')).toBeVisible();
  await expect(card.getByText('Reorder requested')).toBeVisible();
  await card.scrollIntoViewIfNeeded();
  await shot(page, 'standee-campaigns-desktop.png');

  await card.getByLabel('Campaign name').fill('Temple road');
  await card.getByRole('button', { name: 'Add standee' }).click();
  await expect(card.getByText('Temple road')).toBeVisible();
  await expect(card.getByLabel('Campaign name')).toHaveValue('');

  const counter = card.getByRole('listitem').filter({ hasText: 'Counter' });
  await counter.getByRole('button', { name: 'Reorder printed standee' }).click();
  await expect(counter.getByText('Reorder requested')).toBeVisible();
  await expect(counter.getByRole('button', { name: 'Reorder printed standee' })).toHaveCount(0);
});

test('phone 360: standee campaigns fit the screen (B-10, W3-09)', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/standee');
  const card = campaigns(page);
  await expect(card.getByLabel('Diwali gate counts').getByText('46')).toBeVisible();
  await card.scrollIntoViewIfNeeded();
  await expect(card.getByRole('button', { name: 'Add standee' })).toHaveCSS('min-height', '44px');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  await card.getByLabel('Counter counts').scrollIntoViewIfNeeded();
  await shot(page, 'standee-campaigns-360.png');
});
