import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { NOW, answerWith, signIn } from './fake-api';

/**
 * A-14 (W3-08) against the built console, with the support API faked statefully: a support agent
 * works the inbox, takes a ticket, notes, replies with a file and resolves it. The unit tests cover
 * the edge cases; this proves the bundle wires the flow, and leaves the screenshots.
 */

const TID = '01JTICKET00000000000000T01';
const ME = '01JADMIN0000000000000000AD';
const PRIYA = '01JAGENT00000000000000AG01';
const PUT = 'https://s3.todayz.localhost/support/upload';
const at = (minutes: number) => new Date(Date.parse(NOW) + minutes * 60_000).toISOString();

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('A-14: a support agent takes, answers and resolves a business ticket', async ({ page }) => {
  await signIn(page, ['support']);
  const ticket = {
    id: TID,
    number: 'SUP-001001',
    subject: 'Last week’s payout has not arrived',
    category: 'payouts',
    status: 'open',
    priority: 'normal',
    openerType: 'business',
    opener: { id: '01JOWNER00000000000000OW01', name: 'Lakshmi' },
    businessId: '01JB0BZ1A2B3C4D5E6F7G8H9J0',
    businessName: 'Lakshmi Traders',
    storeId: '01JST0RE000000000000000S01',
    storeName: 'Lakshmi Indiranagar',
    orderId: null,
    orderNumber: null,
    assignee: null as { id: string; name: string } | null,
    slaDueAt: at(45) as string | null,
    overdue: false,
    lastMessageAt: at(-15),
    createdAt: at(-15),
  };
  const urgent = {
    ...ticket,
    id: '01JTICKET00000000000000T02',
    number: 'SUP-001002',
    subject: 'My pickup was not ready',
    category: 'order',
    priority: 'urgent',
    openerType: 'customer',
    opener: { id: '01JCUST0MER000000000000C01', name: 'Asha' },
    businessId: null,
    businessName: null,
    storeId: null,
    storeName: null,
    slaDueAt: at(-20),
    overdue: true,
  };
  const messages: unknown[] = [
    {
      id: '01JMSG0000000000000000M001',
      kind: 'reply',
      authorType: 'business',
      author: { id: ticket.opener.id, name: 'Lakshmi' },
      body: 'The payout for last week shows as sent but it is not in our bank account yet.',
      createdAt: at(-15),
      attachments: [],
    },
  ];
  let closeReason: string | null = null;
  let firstResponseAt: string | null = null;
  const detail = () => ({
    ticket,
    order: null,
    firstResponseAt,
    resolvedAt: ticket.status === 'resolved' ? NOW : null,
    closedAt: null,
    closeReason,
    messages,
    agents: [
      { id: PRIYA, name: 'Priya' },
      { id: ME, name: 'Asha Rao' },
    ],
    assistEnabled: false,
    serverNow: NOW,
  });
  const sent: { path: string; body: unknown; key: string | undefined }[] = [];
  let put: { type: string | undefined; size: number } | null = null;
  await page.route(PUT, async (route) => {
    const req = route.request();
    const headers = { 'access-control-allow-origin': req.headers()['origin'] ?? '*', 'access-control-allow-methods': 'PUT', 'access-control-allow-headers': 'content-type' };
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers });
    put = { type: req.headers()['content-type'], size: req.postDataBuffer()?.length ?? 0 };
    return route.fulfill({ status: 200, headers, body: '' });
  });
  await answerWith(page, '/v1/admin/tickets**', (req, url) => {
    const raw = req.postData();
    const body = raw ? (JSON.parse(raw) as Record<string, unknown>) : undefined;
    if (req.method() !== 'GET') sent.push({ path: url.pathname, body, key: req.headers()['idempotency-key'] });
    const base = `/v1/admin/tickets/${TID}`;
    if (url.pathname === '/v1/admin/tickets') {
      const open = ticket.status === 'open' ? [urgent, ticket] : [urgent];
      return { status: 200, body: { items: open, counts: { open: open.length, waiting: 0, resolved: 3, closed: 8, overdue: 1 }, nextCursor: null, serverNow: NOW } };
    }
    if (url.pathname === base) return { status: 200, body: detail() };
    if (url.pathname === `${base}/assign`) {
      ticket.assignee = body?.assigneeId === ME ? { id: ME, name: 'Asha Rao' } : null;
      return { status: 200, body: detail() };
    }
    if (url.pathname === `${base}/priority`) {
      ticket.priority = body?.priority as typeof ticket.priority;
      return { status: 200, body: detail() };
    }
    if (url.pathname === `${base}/attachments`) {
      return {
        status: 201,
        body: {
          attachment: { id: '01JATT0000000000000000A009', fileName: body?.fileName, contentType: body?.contentType, sizeBytes: body?.sizeBytes, url: null },
          upload: { url: PUT, method: 'PUT', headers: { 'Content-Type': body?.contentType, 'Content-Length': String(body?.sizeBytes) }, expiresAt: at(15) },
        },
      };
    }
    if (url.pathname === `${base}/notes` || url.pathname === `${base}/reply`) {
      const kind = url.pathname.endsWith('/notes') ? 'note' : 'reply';
      const files = (body?.attachmentIds as string[] | undefined)?.length
        ? [{ id: '01JATT0000000000000000A009', fileName: 'utr.png', contentType: 'image/png', sizeBytes: 68, url: PUT }]
        : [];
      messages.push({ id: `01JMSG0000000000000000M00${messages.length + 1}`, kind, authorType: 'admin', author: { id: ME, name: 'Asha Rao' }, body: body?.body, createdAt: NOW, attachments: files });
      if (kind === 'reply') {
        Object.assign(ticket, { status: 'waiting', slaDueAt: null });
        firstResponseAt ??= NOW;
      }
      return { status: 200, body: detail() };
    }
    if (url.pathname === `${base}/close`) {
      Object.assign(ticket, { status: body?.outcome, slaDueAt: null });
      closeReason = body?.reason as string;
      return { status: 200, body: detail() };
    }
    return undefined;
  });

  await page.goto('/support');
  const table = page.getByRole('table', { name: 'Tickets' });
  await expect(table.getByText('Overdue by 20 min')).toBeVisible();
  await expect(table.getByText('Due in 45 min')).toBeVisible();
  await expect(page.getByText('1 open ticket is past its reply time.')).toBeVisible();
  await shot(page, 'admin-a14-inbox.png');

  await table.getByRole('link', { name: /SUP-001001/ }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Ticket SUP-001001' })).toBeVisible();
  await page.getByRole('button', { name: 'Assign to me' }).click();
  await expect(page.getByText('Assigned to Asha Rao')).toBeVisible();
  await page.getByLabel('Set priority').selectOption('high');
  await page.getByRole('button', { name: 'Change' }).click();
  await expect(page.getByText('Priority set to High')).toBeVisible();

  await page.getByRole('radio', { name: 'Internal note' }).click();
  await page.getByRole('textbox', { name: 'Internal note' }).fill('Bank batch went out Monday; UTR is in the payout run.');
  await page.getByRole('button', { name: 'Add note' }).click();
  await expect(page.locator('[data-kind="note"]')).toContainText('Bank batch went out Monday');

  await page.getByRole('radio', { name: 'Reply' }).click();
  // A 1×1 PNG, uploaded to the presigned URL before the reply claims it.
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=', 'base64');
  await page.locator('input[type="file"]').setInputFiles({ name: 'utr.png', mimeType: 'image/png', buffer: png });
  await expect(page.getByRole('button', { name: 'Remove utr.png' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Reply to the opener' }).fill('The payout went out on Monday. The UTR is in the attached screenshot.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByRole('link', { name: 'utr.png' })).toBeVisible();
  await expect(page.getByRole('main').getByText('Waiting', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Paused until the opener replies')).toBeVisible();
  await shot(page, 'admin-a14-ticket.png', { fullPage: true });

  await page.getByRole('button', { name: 'Resolve' }).click();
  const dialog = page.getByRole('dialog', { name: 'Resolve SUP-001001?' });
  await dialog.getByRole('textbox').fill('UTR shared with the business');
  await dialog.getByRole('button', { name: 'Resolve' }).click();
  await expect(page.getByText('SUP-001001 resolved')).toBeVisible();

  expect(put).toEqual({ type: 'image/png', size: png.length });
  expect(sent.map((s) => [s.path.replace(`/v1/admin/tickets/${TID}`, ''), s.body])).toEqual([
    ['/assign', { assigneeId: ME }],
    ['/priority', { priority: 'high' }],
    ['/notes', { body: 'Bank batch went out Monday; UTR is in the payout run.' }],
    ['/attachments', { fileName: 'utr.png', contentType: 'image/png', sizeBytes: png.length }],
    ['/reply', { body: 'The payout went out on Monday. The UTR is in the attached screenshot.', attachmentIds: ['01JATT0000000000000000A009'] }],
    ['/close', { outcome: 'resolved', reason: 'UTR shared with the business' }],
  ]);
  for (const s of sent.filter((x) => /\/(notes|reply)$/.test(x.path))) expect(s.key).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
});
