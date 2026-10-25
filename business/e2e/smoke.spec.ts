import { VIEWPORTS, expectFontLoaded, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { INSIGHTS, overview, PERIOD, SERVER_NOW } from '../src/test/insightsData';

/** The self-hosted Figtree file, as Vite names it (figtree-latin-wght-normal-<hash>.woff2). */
const FIGTREE = /\/figtree-[^/]*\.woff2$/;

let guard: PageGuard;

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZCCCCCCCCCCCCCCCCCCCCC';

/**
 * The app is for signed-in staff. The smoke run has no API, so the session refresh is answered here:
 * with a grant (signed in) or a 401 (signed out). The API is on another origin, so the cookie-carrying
 * call needs the preflight and an explicit origin in the answer.
 */
async function answerRefresh(page: Page, signedIn: boolean) {
  await page.route('**/v1/auth/refresh', (route) => {
    const origin = route.request().headers()['origin'] ?? '*';
    const cors = {
      'access-control-allow-origin': origin,
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'content-type, x-tz-client, accept-language',
      'access-control-allow-methods': 'POST, OPTIONS',
    };
    if (route.request().method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors });
    if (!signedIn) {
      return route.fulfill({
        status: 401,
        contentType: 'application/problem+json',
        headers: cors,
        body: JSON.stringify({ type: 'about:blank#unauthorized', title: 'Unauthorized', status: 401, code: 'unauthorized' }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: cors,
      body: JSON.stringify({
        accessToken: 'smoke-token',
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        tokenType: 'Bearer',
        userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
      }),
    });
  });
}

/**
 * Signed in, the shell asks for the staff's businesses, the live orders and (for the owner) today's
 * overview and Demand insights (W3-03). One shop with no orders; any other call is a 404 problem. Registered before the refresh route, which wins for its path.
 */
async function answerApi(page: Page) {
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const headers = {
      'access-control-allow-origin': req.headers()['origin'] ?? '*',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization, content-type, x-tz-client, accept-language, idempotency-key, if-match',
      'access-control-allow-methods': 'GET, POST, PATCH, PUT, DELETE, OPTIONS',
    };
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers });
    const json = (status: number, body: unknown, contentType = 'application/json') =>
      route.fulfill({ status, contentType, headers, body: JSON.stringify(body) });
    const path = new URL(req.url()).pathname;
    if (path === '/v1/biz/businesses') {
      return json(200, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role: 'owner', stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (path.endsWith('/orders')) return json(200, { items: [], nextCursor: null, serverNow: new Date().toISOString() });
    if (path.endsWith('/offers')) return json(200, { items: [], nextCursor: null });
    if (path.endsWith('/overview')) return json(200, overview({ range: new URL(req.url()).searchParams.get('range') ?? 'today', newOrders: [] }));
    if (path.endsWith('/insights')) return json(200, { items: INSIGHTS, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    if (path.endsWith('/stream')) {
      return route.fulfill({ status: 200, contentType: 'text/event-stream', headers, body: 'retry: 600000\nevent: ready\ndata: {}\n\n' });
    }
    return json(404, { type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }, 'application/problem+json');
  });
}

test.beforeEach(async ({ page }, info) => {
  guard = await guardPage(page);
  // The app only tries a refresh after a sign-in left this hint (as sell.spec.ts does). The signed-out
  // test goes without it; about:blank (the Back test) refuses localStorage.
  if (!info.title.startsWith('signed out')) await page.addInitScript(() => {
    try {
      localStorage.setItem('todayz.hasSession.business', '1');
    } catch {
      /* not this app's document */
    }
  });
  await answerApi(page);
  await answerRefresh(page, true);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const sidebar = '.tz-side';
const tabs = '.tz-bnav';

test('phone: lands on Overview with bottom tabs, no sidebar (B-03)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
  await expect(page.locator(tabs)).toBeVisible();
  await expect(page.locator(sidebar)).toBeHidden();
  await expect(page.locator(tabs).getByRole('link')).toHaveCount(5);
  await expect(page.locator(tabs).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
  await expect(page.locator(tabs).getByRole('link', { name: 'Orders' })).not.toHaveAttribute('aria-current');
  await expect(page.getByRole('main')).toHaveAttribute('data-density', 'touch');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-phone.png');
});

test('desktop: sidebar with Overview active, no bottom tabs', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator(sidebar)).toBeVisible();
  await expect(page.locator(tabs)).toBeHidden();
  await expect(page.locator(sidebar).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('main')).not.toHaveAttribute('data-density');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-desktop.png');
});

test('Back after the /overview redirect leaves the app instead of bouncing', async ({ page }) => {
  await page.goto('about:blank');
  await page.goto('/overview');
  await expect(page).toHaveURL(/\/$/);
  await page.goBack();
  await expect(page).toHaveURL('about:blank');
});

test('the Overview tab goes back to / from another section', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.locator(tabs).getByRole('link', { name: 'Overview' }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
});

test('More tab lists the other sections', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await page.locator(tabs).getByRole('link', { name: 'More' }).click();
  await expect(page).toHaveURL(/\/more$/);
  await page.getByRole('region', { name: 'More' }).getByRole('link', { name: 'Offers' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Offers' })).toBeVisible();
});

test('pseudo-locale via ?locale=pseudo survives the /overview redirect', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/overview?locale=pseudo');
  await expect(page).toHaveURL(/\/\?locale=pseudo$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/^\[.+\]$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en-XA');
  await expectFontLoaded(page, guard, 'Figtree', FIGTREE);
  await shot(page, 'business-pseudo-desktop.png');
  await page.setViewportSize(VIEWPORTS.phone);
  await shot(page, 'business-pseudo-phone.png');
});

test('PWA manifest and icons are served locally', async ({ page, request }) => {
  await page.goto('/');
  const manifest = await (await request.get('/manifest.webmanifest')).json();
  expect(manifest).toMatchObject({ name: 'TodayZ Business', lang: 'en-IN', display: 'standalone' });
  for (const icon of manifest.icons as { src: string }[]) {
    expect((await request.get(icon.src)).status(), icon.src).toBe(200);
  }
});

test('signed out: any page opens the sign-in screen (B-01), with no sidebar or tabs', async ({ page }) => {
  await page.unroute('**/v1/auth/refresh');
  await answerRefresh(page, false);
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/orders');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Forders$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Business' })).toBeVisible();
  await expect(page.getByLabel('Mobile number')).toBeVisible();
  await expect(page.locator(tabs)).toHaveCount(0);
  await expect(page.locator(sidebar)).toHaveCount(0);
  await shot(page, 'business-sign-in-phone.png');
});
