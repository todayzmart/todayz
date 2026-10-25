import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { ALL_INSIGHTS, analytics, CUSTOMERS, INSIGHTS, overview, PERIOD, SERVER_NOW, STORE } from '../src/test/insightsData';

// B-03 Overview, B-12 Analytics (Sales and Insights) and B-09 Customers (W3-03) on the
// production-shaped build with a faked API (no stack), signed in as the owner, at 360 px and desktop.
// `TZ_QA_DIR=docs/qa/W3-03 pnpm exec playwright test insights.spec.ts` refreshes the committed shots.

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
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const q = url.searchParams;
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: 'b1', brandName: 'Sri Lakshmi', status: 'verified', role: 'owner', stores: [{ id: 's1', name: 'Sri Lakshmi Provision Stores', slug: 'sri-lakshmi', status: 'live' }] }],
        nextCursor: null,
      });
    }
    if (url.pathname === `${STORE}/overview`) return ok(route, overview({ range: q.get('range') ?? 'today' }));
    if (url.pathname === `${STORE}/insights` && req.method() === 'GET') {
      const all = q.get('all') === '1' ? ALL_INSIGHTS : INSIGHTS;
      const kind = q.get('kind');
      return ok(route, { items: kind ? all.filter((i) => i?.kind === kind) : all, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    }
    if (url.pathname === `${STORE}/analytics`) return ok(route, analytics(q.get('range') ?? '7d'));
    if (url.pathname === `${STORE}/customers`) {
      const segment = q.get('segment') ?? 'all';
      return ok(route, { segment, enough: true, total: CUSTOMERS.length, windowDays: 90, lapseDays: 30, items: CUSTOMERS, nextCursor: null, serverNow: SERVER_NOW });
    }
    if (url.pathname === `${STORE}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: SERVER_NOW });
    if (url.pathname === `${STORE}/coach/cards`) return ok(route, { items: [], nextCursor: null, serverNow: SERVER_NOW });
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

const SIZES = [
  ['desktop', VIEWPORTS.desktop],
  ['360', { width: 360, height: 800 }],
] as const;

for (const [name, size] of SIZES) {
  test(`${name}: Overview with the KPIs, new orders, chart and Demand insights (B-03, W3-03)`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
    const card = page.getByRole('region', { name: 'Demand insights' });
    await expect(card.getByText(/Amul Taaza 500 ml sold 96 this week/)).toBeVisible();
    await expect(card.getByRole('link', { name: /^Restock/ })).toHaveAttribute('href', '/stock?restock=i1&qty=40');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(size.width);
    await shot(page, `overview-${name}.png`);
    await card.scrollIntoViewIfNeeded();
    await shot(page, `overview-insights-${name}.png`);
  });

  test(`${name}: Analytics Insights tab, grouped by kind with the not-enough-data group (B-12, W3-03)`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto('/analytics?tab=insights');
    await expect(page.getByRole('heading', { level: 1, name: 'Analytics' })).toBeVisible();
    await expect(page.getByText(/Tata Salt 1 kg sold 51 this week/)).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(size.width);
    await shot(page, `analytics-insights-${name}.png`);
  });

  test(`${name}: Analytics Sales tab (B-12, W3-03)`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto('/analytics?tab=sales');
    await expect(page.getByRole('heading', { name: 'Sales by day' })).toBeVisible();
    await expect(page.getByText('14 of 40 linked customers came back')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(size.width);
    await shot(page, `analytics-sales-${name}.png`);
  });

  test(`${name}: Customers, Slipping away (B-09, W3-03)`, async ({ page }) => {
    await page.setViewportSize(size);
    await page.goto('/customers?segment=slipping');
    await expect(page.getByText('Meera S.')).toBeVisible();
    await expect(page.getByText('••••••7788')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(size.width);
    await shot(page, `customers-slipping-${name}.png`);
  });
}
