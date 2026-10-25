import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// B-20 Asks inbox (W3-16) on the production-shaped build with a faked API (no stack), signed in as
// the owner: open asks with their countdown, a Yes at the shop's own price with a ready-by time, and
// the reply landing under "Done". `TZ_QA_DIR=docs/qa/W3-16 pnpm exec playwright test asks.spec.ts`.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const base = `/v1/biz/businesses/${BID}/stores/${SID}`;
const at = (min: number) => new Date(Date.now() + min * 60_000).toISOString();

const ask = (askId: string, over: Record<string, unknown> = {}) => ({
  askId,
  itemName: 'Amul Butter 100 g',
  productId: '01J9ZPPPPPPPPPPPPPPPPPPPP1',
  storeItemId: '01J9ZIIIIIIIIIIIIIIIIIIII1',
  quantity: 2,
  needBy: 'today',
  distanceBand: '1_3km',
  sentAt: at(-2),
  replyBy: at(8),
  status: 'open',
  answer: null,
  pricePaise: null,
  readyBy: null,
  repliedAt: null,
  reported: false,
  available: 9,
  listedPricePaise: 5800,
  mrpPaise: 6000,
  ...over,
});

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

/** A stateful inbox: a reply moves the ask from open to done. Returns what the page sent. */
async function fakeApi(page: Page) {
  let open = [
    ask('01J9ZASKAAAAAAAAAAAAAAAAA1'),
    ask('01J9ZASKAAAAAAAAAAAAAAAAA2', { itemName: 'Kissan mixed fruit jam 500 g', productId: null, storeItemId: null, quantity: 1, needBy: null, distanceBand: 'lt_1km', available: null, listedPricePaise: null, mrpPaise: null, replyBy: at(5) }),
  ];
  let done = [ask('01J9ZASKAAAAAAAAAAAAAAAAA3', { itemName: 'Toor dal 1 kg', status: 'missed', sentAt: at(-90), replyBy: at(-80) })];
  const sent: { path: string; body: unknown; key: string | undefined }[] = [];
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (url.pathname === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: at(60), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (url.pathname === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'active', role: 'owner', stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    if (url.pathname.endsWith('/stream')) return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (url.pathname === `${base}/settings`) return ok(route, { asksMuted: false });
    if (url.pathname === `${base}/orders`) return ok(route, { items: [], nextCursor: null, serverNow: new Date().toISOString() });
    if (url.pathname === `${base}/asks`) {
      const list = url.searchParams.get('status') === 'open' ? open : [...done, ...open];
      return ok(route, { asks: list, openCount: open.length, nextCursor: null, serverNow: new Date().toISOString() });
    }
    const reply = url.pathname.match(new RegExp(`^${base}/asks/([^/]+)/reply$`));
    if (reply && req.method() === 'POST') {
      const body = JSON.parse(req.postData() ?? '{}') as { answer: string; pricePaise?: number; readyBy?: string };
      sent.push({ path: url.pathname, body, key: req.headers()['idempotency-key'] });
      const was = open.find((a) => a.askId === reply[1]);
      if (!was) return ok(route, { type: 'about:blank#ask_closed', title: 'ask_closed', status: 409, code: 'ask_closed' }, 409);
      const now = ask(was.askId, { ...was, status: 'replied', answer: body.answer, pricePaise: body.answer === 'yes' ? (body.pricePaise ?? was.listedPricePaise) : null, readyBy: body.readyBy ?? null, repliedAt: at(0) });
      open = open.filter((a) => a.askId !== was.askId);
      done = [now, ...done];
      return ok(route, now);
    }
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'Not found', status: 404, code: 'not_found' }) });
  });
  return sent;
}

let guard: PageGuard;
let sent: Awaited<ReturnType<typeof fakeApi>>;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
  sent = await fakeApi(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

test('desktop: the inbox, a Yes at the shop price, then Done (B-20)', async ({ page }) => {
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/orders?tab=asks');
  const tab = page.getByRole('tablist', { name: 'Order status' }).getByRole('tab', { name: /Asks/ });
  await expect(tab).toHaveAttribute('aria-selected', 'true');
  // Scoped to the open card: the missed Toor dal ask has the same facts and, at some IST hours,
  // sits under "Done today" too.
  await expect(page.getByRole('article', { name: 'Amul Butter 100 g' }).getByText('2 wanted · Need it today · 1–3 km')).toBeVisible();
  await expect(page.getByTestId('ask-countdown').first()).toHaveText(/\d{2}:\d{2}/);
  // No customer name, phone or exact distance anywhere on the card.
  await expect(page.getByRole('article', { name: 'Amul Butter 100 g' })).not.toContainText(/\+91|Meera| m\b/);
  await shot(page, 'business-b20-inbox.png');

  await page.getByRole('button', { name: 'Reply to the ask for Amul Butter 100 g' }).click();
  const sheet = page.getByRole('dialog');
  const price = sheet.getByRole('textbox', { name: /Price for one/ });
  await expect(price).toHaveValue('58');
  await expect(sheet.getByText(/MRP ₹60/)).toBeVisible();
  await price.fill('55');
  await shot(page, 'business-b20-reply.png');
  await sheet.getByRole('button', { name: 'Yes, I have it' }).click();
  await expect(sheet).toHaveCount(0);
  expect(sent).toHaveLength(1);
  expect(sent[0]?.body).toMatchObject({ answer: 'yes', pricePaise: 5500 });
  expect(sent[0]?.key).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);

  await expect(page.getByRole('article', { name: 'Amul Butter 100 g' })).toHaveCount(0);
  const doneList = page.getByRole('region', { name: 'Done today' });
  await expect(doneList.getByText('Amul Butter 100 g')).toBeVisible();
  await expect(doneList.getByText('₹55')).toBeVisible();
  await shot(page, 'business-b20-done.png', { fullPage: true });
});

test('360 px: the cards and the reply sheet fit (B-20)', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 760 });
  await page.goto('/orders?tab=asks');
  await expect(page.getByRole('article', { name: 'Kissan mixed fruit jam 500 g' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  await shot(page, 'business-b20-inbox-360.png');
  await page.getByRole('button', { name: 'Reply to the ask for Kissan mixed fruit jam 500 g' }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByRole('link', { name: 'Add it to your stock' })).toBeVisible();
  await shot(page, 'business-b20-reply-unlisted-360.png');
});
