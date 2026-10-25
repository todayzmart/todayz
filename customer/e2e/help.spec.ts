import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';

// C-19 help (W3-08, D-226) at 360 px on the e2e build with a faked API (no stack): the account
// entry, opening a ticket from an order with a photo (SC-SP-01, SP-07), support's reply shown as
// "TodayZ support" (SP-03), the customer's reply reopening it and a closed ticket taking no reply
// (SP-04), and another person's ticket being not found (SP-08).
// `TZ_QA_DIR=docs/qa/W3-08 pnpm exec playwright test help.spec.ts` refreshes the committed shots.

const PHONE = { width: 360, height: 780 } as const;
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const ORDER = '01J9ZORDERAAAAAAAAAAAAAAAA';
const PUT_URL = 'https://s3.todayz.localhost/todayz-private/support/customers/01J9ZUUUUUUUUUUUUUUUUUUUUU?X-Amz-Signature=fake';
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
const ok = (route: Route, body: unknown, status = 200, headers: Record<string, string> = {}) =>
  route.fulfill({ status, contentType: 'application/json', headers: { ...cors(route), ...headers }, body: JSON.stringify(body) });
const problem = (route: Route, status: number, code: string) =>
  route.fulfill({ status, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: `about:blank#${code}`, title: code, status, code }) });

type Attachment = { id: string; fileName: string; contentType: string; sizeBytes: number; url: string | null };
interface Message {
  id: string;
  from: 'customer' | 'support';
  body: string;
  createdAt: string;
  attachments: Attachment[];
}
interface Ticket {
  id: string;
  number: string;
  subject: string;
  category: string;
  status: 'open' | 'waiting' | 'resolved' | 'closed';
  storeName: string | null;
  orderId: string | null;
  orderNumber: string | null;
  lastMessageAt: string;
  createdAt: string;
}

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

const order = {
  id: ORDER,
  number: 'R-1042',
  store: { id: '01J9ZSTOREAAAAAAAAAAAAAAAA', slug: 'raju-kirana', name: 'Raju Kirana', addressLine: '12 Main Road', locality: 'Indiranagar' },
  fulfilment: 'pickup',
  status: 'collected',
  statusReason: null,
  statusNote: null,
  lines: [
    { id: '01J9ZLINEAAAAAAAAAAAAAAAA1', storeItemId: '01J9ZITEMAAAAAAAAAAAAAAAA1', name: 'Toor dal', variant: null, packSize: 1, unit: 'kg', imageKey: null, unitPricePaise: 8400, quantityRequested: 2, quantityAccepted: 2, lineTotalPaise: 16800, priceSource: 'catalog' },
  ],
  subtotalPaise: 16800,
  discountPaise: 0,
  itemSubtotalPaise: 16800,
  itemsAfterOffersPaise: 16800,
  creditsUsed: 0,
  deliveryFeePaise: 0,
  totalPaise: 16800,
  cashbackCredits: 0,
  paymentStatus: 'paid_at_shop',
  paymentMethod: null,
  code: null,
  codeLocked: false,
  canExtend: false,
  askReplyId: null,
  acceptBy: null,
  partialConfirmBy: null,
  holdUntil: null,
  extendedAt: null,
  placedAt: ago(120),
  acceptedAt: ago(118),
  collectedAt: ago(90),
  timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: ago(120) }],
  version: 4,
  serverNow: NOW,
};

/** A stateful fake: tickets live in memory so opening, replying and the list agree. */
async function fakeApi(page: Page) {
  const tickets: Ticket[] = [
    {
      id: '01J9ZTTTTTTTTTTTTTTTTTTT40',
      number: 'SUP-000040',
      subject: 'Credits from last week missing',
      category: 'credits',
      status: 'closed',
      storeName: null,
      orderId: null,
      orderNumber: null,
      lastMessageAt: ago(3 * 24 * 60),
      createdAt: ago(4 * 24 * 60),
    },
  ];
  const threads = new Map<string, Message[]>([
    [
      '01J9ZTTTTTTTTTTTTTTTTTTT40',
      [
        { id: 'm1', from: 'customer', body: 'My cashback from the Raju Kirana order did not come.', createdAt: ago(4 * 24 * 60), attachments: [] },
        { id: 'm2', from: 'support', body: 'It is in your wallet now. Sorry for the wait.', createdAt: ago(3 * 24 * 60), attachments: [] },
      ],
    ],
  ]);
  const uploads = new Map<string, Attachment>();
  const puts: Record<string, string>[] = [];
  const posts: { path: string; key: string | undefined; body: unknown }[] = [];
  let seq = 41;
  const thread = (t: Ticket) => ({ ticket: t, messages: threads.get(t.id) ?? [], serverNow: new Date().toISOString() });
  const post = (t: Ticket, m: Message, status: Ticket['status']) => {
    threads.get(t.id)?.push(m);
    t.status = status;
    t.lastMessageAt = m.createdAt;
  };
  const files = (ids: string[] | undefined) =>
    (ids ?? []).flatMap((id) => {
      const a = uploads.get(id);
      return a ? [{ ...a, url: `https://s3.todayz.localhost/todayz-private/${id}?sig=get` }] : [];
    });

  await page.route('https://s3.todayz.localhost/**', (route) => {
    if (route.request().method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    puts.push(route.request().headers());
    return route.fulfill({ status: 200, headers: cors(route), body: '' });
  });
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    const p = new URL(req.url()).pathname;
    if (p === '/v1/auth/refresh')
      return ok(route, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: USER });
    if (p === '/v1/me') return ok(route, { id: USER, phoneMasked: '+91 ••••• •0021', name: 'Asha', preferredLang: 'en' }, 200, { etag: '"1"' });
    if (p === '/v1/me/addresses') return ok(route, { addresses: [] });
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (p === '/v1/me/orders')
      return ok(route, {
        items: [{ id: ORDER, number: order.number, store: order.store, fulfilment: 'pickup', status: 'collected', statusReason: null, itemCount: 2, totalPaise: 16800, acceptBy: null, holdUntil: null, placedAt: order.placedAt, version: 4 }],
        nextCursor: null,
        serverNow: NOW,
      });
    if (p === `/v1/me/orders/${ORDER}`) return ok(route, order);
    if (p === '/v1/me/support/tickets' && req.method() === 'GET') {
      const items = [...tickets].sort((a, b) => b.lastMessageAt.localeCompare(a.lastMessageAt));
      return ok(route, { items, nextCursor: null });
    }
    if (p === '/v1/me/support/attachments' && req.method() === 'POST') {
      const b = req.postDataJSON() as { fileName: string; contentType: string; sizeBytes: number };
      const a = { id: `01J9ZUUUUUUUUUUUUUUUUUU${String(uploads.size).padStart(3, '0')}`, fileName: b.fileName, contentType: b.contentType, sizeBytes: b.sizeBytes, url: null };
      uploads.set(a.id, a);
      return ok(route, { attachment: a, upload: { url: PUT_URL, method: 'PUT', headers: { 'Content-Type': b.contentType, 'Content-Length': String(b.sizeBytes) }, expiresAt: new Date(Date.now() + 900_000).toISOString() } }, 201);
    }
    if (p === '/v1/me/support/tickets' && req.method() === 'POST') {
      const b = req.postDataJSON() as { category: string; subject: string; body: string; orderId?: string; attachmentIds?: string[] };
      posts.push({ path: p, key: req.headers()['idempotency-key'], body: b });
      const at = new Date().toISOString();
      const mine = b.orderId === ORDER;
      const t: Ticket = {
        id: `01J9ZTTTTTTTTTTTTTTTTTTT${seq}`,
        number: `SUP-0000${seq}`,
        subject: b.subject,
        category: b.category,
        status: 'open',
        storeName: mine ? order.store.name : null,
        orderId: mine ? ORDER : null,
        orderNumber: mine ? order.number : null,
        lastMessageAt: at,
        createdAt: at,
      };
      seq += 1;
      tickets.push(t);
      threads.set(t.id, [{ id: `m${Date.now()}`, from: 'customer', body: b.body, createdAt: at, attachments: files(b.attachmentIds) }]);
      return ok(route, thread(t), 201);
    }
    const m = /^\/v1\/me\/support\/tickets\/([^/]+)\/messages$/.exec(p);
    if (m) {
      const t = tickets.find((x) => x.id === m[1]);
      if (!t) return problem(route, 404, 'not_found');
      if (req.method() === 'GET') return ok(route, thread(t));
      if (t.status === 'closed') return problem(route, 409, 'invalid_transition');
      const b = req.postDataJSON() as { body: string; attachmentIds?: string[] };
      posts.push({ path: p, key: req.headers()['idempotency-key'], body: b });
      post(t, { id: `m${Date.now()}`, from: 'customer', body: b.body, createdAt: new Date().toISOString(), attachments: files(b.attachmentIds) }, 'open');
      return ok(route, thread(t), 201);
    }
    return problem(route, 404, 'not_found');
  });
  return {
    tickets,
    puts,
    posts,
    supportReply(id: string, body: string) {
      const t = tickets.find((x) => x.id === id);
      if (t) post(t, { id: `m${Date.now()}`, from: 'support', body, createdAt: new Date().toISOString(), attachments: [] }, 'waiting');
    },
  };
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.customer', '1'));
});

// The one 404 a test asks for on purpose (someone else's ticket) is the only console error allowed.
let expected404 = 0;

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  const notFound = guard.errors.filter((e) => e.includes('status of 404'));
  expect(guard.errors.filter((e) => !e.includes('status of 404')), 'console and page errors').toEqual([]);
  expect(notFound, '404s').toHaveLength(expected404);
  expected404 = 0;
});

test('360: from an order to a ticket with a photo, support replies, the customer replies (C-19, SP-01, 03, 04, 07)', async ({ page }) => {
  const api = await fakeApi(page);
  await page.goto(`/orders/${ORDER}`);
  await page.getByRole('link', { name: 'Get help with this order' }).click();

  await expect(page.getByRole('heading', { name: 'New ticket' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/help/new\\?orderId=${ORDER}$`));
  await expect(page.getByLabel('What is it about?')).toHaveValue('order');
  await expect(page.getByLabel('Which order? (optional)')).toHaveValue(ORDER);
  await page.getByLabel('Subject').fill('Toor dal pack was torn');
  await page.getByLabel('What happened?').fill('One of the two toor dal packs was torn and half empty. Photo attached.');
  await page.locator('input[type=file]').setInputFiles({ name: 'dal.png', mimeType: 'image/png', buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 1, 2, 3, 4]) });
  await expect(page.getByRole('list', { name: 'Files' }).getByText('dal.png')).toBeVisible();
  expect(api.puts).toHaveLength(1);
  expect(api.puts[0]?.['content-type']).toBe('image/png');
  await shot(page, 'customer-c19-new-ticket-360.png', { fullPage: true });

  await page.getByRole('button', { name: 'Send to support' }).click();
  await expect(page.getByRole('heading', { name: 'Ticket SUP-000041' })).toBeVisible();
  await expect(page).toHaveURL(/\/help\/01J9ZTTTTTTTTTTTTTTTTTTT41$/);
  await expect(page.getByText(/R-1042/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'dal.png' })).toBeVisible();
  expect(api.posts[0]?.key).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
  expect(api.posts[0]?.body).toMatchObject({ category: 'order', orderId: ORDER, attachmentIds: ['01J9ZUUUUUUUUUUUUUUUUUU000'] });

  // Support answers on A-14 (SP-03); the customer sees "TodayZ support", never the agent's name.
  api.supportReply('01J9ZTTTTTTTTTTTTTTTTTTT41', 'Sorry about that. Raju Kirana will replace the pack when you next visit.');
  await page.reload();
  const messages = page.getByRole('list', { name: 'Messages' });
  await expect(messages.locator(':scope > li').last()).toContainText('TodayZ support');
  await expect(page.getByText('Replied', { exact: true })).toBeVisible();
  await expect(page.getByText('Sending a reply opens the ticket again.')).toBeVisible();
  await shot(page, 'customer-c19-thread-360.png', { fullPage: true });

  await page.getByLabel('Your message').fill('Thank you.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(messages.locator(':scope > li')).toHaveCount(3);
  await expect(page.getByLabel('Your message')).toHaveValue('');
  expect(api.tickets.find((t) => t.number === 'SUP-000041')?.status).toBe('open');
});

test('360: the account entry, the list, a closed ticket and someone else\'s (C-19, SP-04, SP-08)', async ({ page }) => {
  await fakeApi(page);
  await page.goto('/account');
  await page.getByRole('link', { name: 'Get help or see your tickets' }).click();
  await expect(page.getByRole('heading', { name: 'Help', exact: true })).toBeVisible();
  const row = page.getByRole('list', { name: 'Tickets' }).getByRole('link', { name: /SUP-000040/ });
  await expect(row).toContainText('Closed');
  await shot(page, 'customer-c19-tickets-360.png');

  await row.click();
  await expect(page.getByText('This ticket is closed and takes no more messages.')).toBeVisible();
  await expect(page.getByLabel('Your message')).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Open a new ticket' })).toBeVisible();
  await shot(page, 'customer-c19-closed-360.png', { fullPage: true });

  expected404 = 1;
  await page.goto('/help/01J9ZTTTTTTTTTTTTTTTTTTT99');
  await expect(page.getByText('Ticket not found')).toBeVisible();
});

test('360: signed out, help asks to sign in', async ({ page }) => {
  await page.addInitScript(() => localStorage.removeItem('todayz.hasSession.customer'));
  await fakeApi(page);
  await page.goto('/help');
  await expect(page.getByRole('heading', { name: 'Sign in to get help' })).toBeVisible();
  await shot(page, 'customer-c19-signed-out-360.png');
});
