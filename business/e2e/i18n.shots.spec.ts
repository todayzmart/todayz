// W4-05 QA evidence: main screens in one language on a phone. Runs only with TZ_LANG (en|hi|kn|te|ta) and TZ_QA_DIR, e.g.
// `TZ_LANG=hi TZ_QA_DIR=docs/qa/W4-05/screens/business/hi pnpm exec playwright test e2e/i18n.shots.spec.ts`.
// The API is answered here as in smoke.spec.ts: one shop, no orders; any other call is a 404 problem.
import { VIEWPORTS, shot } from '@todayz/config/playwright';
import { test, type Page } from '@playwright/test';
import { INSIGHTS, overview, PERIOD, SERVER_NOW } from '../src/test/insightsData';

const LANG = process.env['TZ_LANG'] ?? '';
test.skip(!LANG || !process.env['TZ_QA_DIR'], 'set TZ_LANG and TZ_QA_DIR to refresh the screenshots');

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZCCCCCCCCCCCCCCCCCCCCC';

const SCREENS: [string, string][] = [
  ['overview', '/'],
  ['orders', '/orders'],
  ['stock', '/stock'],
  ['offers', '/offers'],
  ['customers', '/customers'],
  ['help', '/help'],
];

async function answerApi(page: Page) {
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const origin = req.headers()['origin'] ?? '*';
    const headers = {
      'access-control-allow-origin': origin,
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization, content-type, x-tz-client, accept-language, idempotency-key, if-match',
      'access-control-allow-methods': 'GET, POST, PATCH, PUT, DELETE, OPTIONS',
    };
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers });
    const json = (status: number, body: unknown, contentType = 'application/json') =>
      route.fulfill({ status, contentType, headers, body: JSON.stringify(body) });
    const url = new URL(req.url());
    const path = url.pathname;
    if (path === '/v1/auth/refresh') {
      return json(200, {
        accessToken: 'shots-token',
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        tokenType: 'Bearer',
        userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
      });
    }
    if (path === '/v1/biz/businesses') {
      return json(200, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role: 'owner', stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (path.endsWith('/orders')) return json(200, { items: [], nextCursor: null, serverNow: new Date().toISOString() });
    if (path.endsWith('/offers')) return json(200, { items: [], nextCursor: null });
    if (path.endsWith('/overview')) return json(200, overview({ range: url.searchParams.get('range') ?? 'today', newOrders: [] }));
    if (path.endsWith('/insights')) return json(200, { items: INSIGHTS, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    if (path.endsWith('/coach/cards')) return json(200, { items: [], nextCursor: null, serverNow: SERVER_NOW });
    if (path.endsWith('/asks')) return json(200, { asks: [], openCount: 0, nextCursor: null, serverNow: SERVER_NOW });
    if (path.endsWith('/stream')) {
      return route.fulfill({ status: 200, contentType: 'text/event-stream', headers, body: 'retry: 600000\nevent: ready\ndata: {}\n\n' });
    }
    return json(404, { type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }, 'application/problem+json');
  });
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript((lang) => {
    try {
      localStorage.setItem('todayz.lang', lang);
      localStorage.setItem('todayz.hasSession.business', '1');
    } catch {
      /* not this app's document */
    }
  }, LANG);
  await answerApi(page);
});

for (const [name, path] of SCREENS) {
  test(`${name} in ${LANG}`, async ({ page }) => {
    await page.setViewportSize(VIEWPORTS.phone);
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await page.evaluate(() => document.fonts.ready);
    await shot(page, `${name}.png`);
  });
}
