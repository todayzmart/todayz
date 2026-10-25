import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { overview } from '../src/test/insightsData';

// B-15 help and tickets (W3-08) on the production-shaped build with a faked API (no stack): the
// guide for every role, opening a ticket with a store, an order and a photo (SC-SP-01, SP-07),
// support's reply shown as "TodayZ support" (SP-03), the shop's reply reopening it and a closed
// ticket taking no reply (SP-04), and a cashier with no tickets tab (SP-08).
// `TZ_QA_DIR=docs/qa/W3-08 pnpm exec playwright test help.spec.ts` refreshes the committed shots.

const BID = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const SID = '01J9ZSSSSSSSSSSSSSSSSSSSSS';
const biz = `/v1/biz/businesses/${BID}`;
const PUT_URL = 'https://s3.todayz.localhost/todayz-private/support/01J9ZUUUUUUUUUUUUUUUUUUUUU?X-Amz-Signature=fake';

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

const problem = (route: Route, status: number, code: string) =>
  route.fulfill({ status, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: `about:blank#${code}`, title: code, status, code }) });

interface Message {
  id: string;
  from: 'business' | 'support';
  authorName: string | null;
  body: string;
  createdAt: string;
  attachments: { id: string; fileName: string; contentType: string; sizeBytes: number; url: string | null }[];
}

interface Ticket {
  id: string;
  number: string;
  subject: string;
  category: string;
  status: 'open' | 'waiting' | 'resolved' | 'closed';
  storeId: string | null;
  storeName: string | null;
  orderId: string | null;
  orderNumber: string | null;
  lastMessageAt: string;
  createdAt: string;
}

const ORDERS = [
  { id: '01J9ZOOOOOOOOOOOOOOOOOOO01', number: 'TZ-4821', status: 'done', customerName: 'Priya', totalPaise: 64500, placedAt: '2026-10-24T17:40:00Z' },
  { id: '01J9ZOOOOOOOOOOOOOOOOOOO02', number: 'TZ-4807', status: 'missed', customerName: 'Arjun', totalPaise: 18900, placedAt: '2026-10-24T12:10:00Z' },
];

/** A stateful fake: the tickets live in memory so opening, replying and the list agree. */
async function fakeApi(page: Page, role: 'owner' | 'cashier' = 'owner') {
  const tickets: Ticket[] = [
    {
      id: '01J9ZTTTTTTTTTTTTTTTTTTT01',
      number: 'SUP-000041',
      subject: 'October statement total looks low',
      category: 'credits',
      status: 'closed',
      storeId: SID,
      storeName: 'Asha Stores, Indiranagar',
      orderId: null,
      orderNumber: null,
      lastMessageAt: '2026-10-20T09:00:00Z',
      createdAt: '2026-10-18T09:00:00Z',
    },
  ];
  const threads = new Map<string, Message[]>([
    [
      '01J9ZTTTTTTTTTTTTTTTTTTT01',
      [
        { id: 'm1', from: 'business', authorName: 'Asha', body: 'The statement shows fewer credits than I expected.', createdAt: '2026-10-18T09:00:00Z', attachments: [] },
        { id: 'm2', from: 'support', authorName: null, body: 'Two orders were cancelled by the customer, so their credits came back. The total is right.', createdAt: '2026-10-19T10:00:00Z', attachments: [] },
      ],
    ],
  ]);
  const uploads = new Map<string, Message['attachments'][number]>();
  let seq = 42;
  const thread = (t: Ticket) => ({ ticket: t, messages: threads.get(t.id) ?? [], serverNow: new Date().toISOString() });
  /** Adds a message to the ticket's thread and moves it on, as the API does. */
  const post = (t: Ticket, m: Message, status: Ticket['status']) => {
    threads.set(t.id, [...(threads.get(t.id) ?? []), m]);
    t.status = status;
    t.lastMessageAt = m.createdAt;
  };
  const state = {
    tickets,
    puts: [] as Record<string, string>[],
    /** Support answers on A-14 (SP-03): the ticket moves to waiting. */
    supportReply(id: string, body: string) {
      const t = tickets.find((x) => x.id === id);
      if (!t) throw new Error(`no ticket ${id}`);
      post(t, { id: `m${Date.now()}`, from: 'support', authorName: null, body, createdAt: new Date().toISOString(), attachments: [] }, 'waiting');
    },
  };

  await page.route('https://s3.todayz.localhost/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    state.puts.push(req.headers());
    return route.fulfill({ status: 200, headers: cors(route) });
  });

  await page.route('**/v1/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const p = url.pathname;
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (p === '/v1/auth/refresh') {
      return ok(route, { accessToken: 'smoke-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
    }
    if (p === '/v1/biz/businesses') {
      return ok(route, {
        items: [{ id: BID, brandName: 'Asha Stores', status: 'verified', role, stores: [{ id: SID, name: 'Asha Stores, Indiranagar', slug: 'asha-indiranagar', status: 'live' }] }],
        invitations: [],
      });
    }
    // The owner's sidebar reads today's overview for the Stock badge and the setup card (W3-03).
    if (p === `${biz}/stores/${SID}/overview`) return ok(route, overview({ newOrders: [] }));
    if (p === `${biz}/stores/${SID}/orders`) {
      const status = url.searchParams.get('status');
      return ok(route, { items: ORDERS.filter((o) => o.status === status), nextCursor: null, serverNow: new Date().toISOString() });
    }
    if (p.startsWith(`${biz}/support`) && role === 'cashier') return problem(route, 403, 'forbidden');
    if (p === `${biz}/support/tickets` && req.method() === 'GET') {
      const items = [...tickets].sort((a, b) => b.lastMessageAt.localeCompare(a.lastMessageAt));
      return ok(route, { items, nextCursor: null });
    }
    if (p === `${biz}/support/attachments` && req.method() === 'POST') {
      const b = req.postDataJSON() as { fileName: string; contentType: string; sizeBytes: number };
      const a = { id: `01J9ZUUUUUUUUUUUUUUUUUU${String(uploads.size).padStart(3, '0')}`, fileName: b.fileName, contentType: b.contentType, sizeBytes: b.sizeBytes, url: null };
      uploads.set(a.id, a);
      return ok(route, { attachment: a, upload: { url: PUT_URL, method: 'PUT', headers: { 'Content-Type': b.contentType, 'Content-Length': String(b.sizeBytes) }, expiresAt: new Date(Date.now() + 900_000).toISOString() } }, 201);
    }
    const files = (ids: string[] | undefined) =>
      (ids ?? []).flatMap((id) => {
        const a = uploads.get(id);
        return a ? [{ ...a, url: `https://s3.todayz.localhost/todayz-private/${id}?sig=get` }] : [];
      });
    if (p === `${biz}/support/tickets` && req.method() === 'POST') {
      const b = req.postDataJSON() as { category: string; subject: string; body: string; storeId?: string; orderId?: string; attachmentIds?: string[] };
      const at = new Date().toISOString();
      const order = ORDERS.find((o) => o.id === b.orderId);
      const t: Ticket = {
        id: `01J9ZTTTTTTTTTTTTTTTTTTT${seq}`,
        number: `SUP-0000${seq}`,
        subject: b.subject,
        category: b.category,
        status: 'open',
        storeId: b.storeId ?? null,
        storeName: b.storeId ? 'Asha Stores, Indiranagar' : null,
        orderId: order?.id ?? null,
        orderNumber: order?.number ?? null,
        lastMessageAt: at,
        createdAt: at,
      };
      seq += 1;
      tickets.push(t);
      threads.set(t.id, [{ id: `m${Date.now()}`, from: 'business', authorName: 'Asha', body: b.body, createdAt: at, attachments: files(b.attachmentIds) }]);
      return ok(route, thread(t), 201);
    }
    const m = /\/support\/tickets\/([^/]+)\/messages$/.exec(p);
    if (m) {
      const t = tickets.find((x) => x.id === m[1]);
      if (!t) return problem(route, 404, 'not_found');
      if (req.method() === 'GET') return ok(route, thread(t));
      if (t.status === 'closed') return problem(route, 409, 'invalid_transition');
      const b = req.postDataJSON() as { body: string; attachmentIds?: string[] };
      post(t, { id: `m${Date.now()}`, from: 'business', authorName: 'Asha', body: b.body, createdAt: new Date().toISOString(), attachments: files(b.attachmentIds) }, 'open');
      return ok(route, thread(t), 201);
    }
    return problem(route, 404, 'not_found');
  });
  return state;
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.business', '1'));
  guard = await guardPage(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('desktop: the guide, opening a ticket with an order and a photo, support replies, the shop replies (B-15, SP-01, 03, 04, 07)', async ({ page }) => {
  const api = await fakeApi(page);
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/help');
  await expect(page.getByRole('heading', { name: 'How TodayZ works for your shop' })).toBeVisible();
  await page.getByText('What happens if I do not accept an order in time?').click();
  await shot(page, 'business-b15-guide-desktop.png');

  await page.getByRole('tab', { name: 'Tickets' }).click();
  await expect(page.getByRole('link', { name: /SUP-000041/ })).toBeVisible();
  await page.getByRole('link', { name: 'New ticket' }).first().click();

  await expect(page.getByRole('heading', { name: 'New ticket' })).toBeVisible();
  await page.getByLabel('What is it about?').selectOption('order');
  await page.getByLabel('Subject').fill('Customer says an item was missing');
  await expect(page.getByLabel('Which shop?')).toHaveValue(SID);
  await expect(page.getByLabel('Which order?').locator('option', { hasText: 'TZ-4821' })).toHaveCount(1);
  await page.getByLabel('Which order?').selectOption('01J9ZOOOOOOOOOOOOOOOOOOO01');
  await page.getByLabel('What happened?').fill('Priya says the 1 kg sugar was not in the bag. We packed it; the photo shows the bill.');
  await page.locator('input[type=file]').setInputFiles({ name: 'bill.png', mimeType: 'image/png', buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 1, 2, 3, 4]) });
  await expect(page.getByRole('list', { name: 'Files' }).getByText('bill.png')).toBeVisible();
  expect(api.puts).toHaveLength(1);
  expect(api.puts[0]?.['content-type']).toBe('image/png');
  await shot(page, 'business-b15-new-ticket-desktop.png', { fullPage: true });

  await page.getByRole('button', { name: 'Send to support' }).click();
  await expect(page.getByRole('heading', { name: 'Ticket SUP-000042' })).toBeVisible();
  await expect(page).toHaveURL(/\/help\/01J9ZTTTTTTTTTTTTTTTTTTT42$/);
  await expect(page.getByText(/TZ-4821/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'bill.png' })).toBeVisible();

  // Support answers on A-14 (SP-03); the shop sees "TodayZ support", never the agent's name.
  api.supportReply('01J9ZTTTTTTTTTTTTTTTTTTT42', 'Thanks. We have checked the bill photo and closed the complaint in your favour; no credits were taken.');
  await page.reload();
  const messages = page.getByRole('list', { name: 'Messages' });
  await expect(messages.locator(':scope > li').last()).toContainText('TodayZ support');
  await expect(page.getByText('Sending a reply opens the ticket again.')).toBeVisible();
  await shot(page, 'business-b15-thread-desktop.png', { fullPage: true });

  await page.getByLabel('Your message').fill('Thank you.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(messages.locator(':scope > li')).toHaveCount(3);
  await expect(page.getByLabel('Your message')).toHaveValue('');
  expect(api.tickets.find((t) => t.number === 'SUP-000042')?.status).toBe('open');
});

test('desktop: a closed ticket takes no reply and offers a new one (B-15, SP-04)', async ({ page }) => {
  await fakeApi(page);
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/help/01J9ZTTTTTTTTTTTTTTTTTTT01');
  await expect(page.getByRole('heading', { name: 'Ticket SUP-000041' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open a new ticket' })).toBeVisible();
  await expect(page.getByLabel('Your message')).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Messages' })).toContainText('TodayZ support');
});

test('phone 360: a cashier sees the guide and no tickets (B-15, SP-08)', async ({ page }) => {
  await fakeApi(page, 'cashier');
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/help');
  await expect(page.getByText('Tickets are opened by the owner or a manager. Ask one of them to contact support for you.')).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Tickets' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'New ticket' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  await shot(page, 'business-b15-cashier-360.png');
});

test('phone 360: an owner opens a ticket from the list (B-15)', async ({ page }) => {
  await fakeApi(page);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/help?tab=tickets');
  await expect(page.getByRole('link', { name: /SUP-000041/ })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  await shot(page, 'business-b15-tickets-360.png');
  await page.getByRole('link', { name: /SUP-000041/ }).click();
  await expect(page.getByRole('heading', { name: 'Ticket SUP-000041' })).toBeVisible();
});
