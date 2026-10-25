import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BIZ = '/v1/biz/businesses/b1';
const TICKETS = `${BIZ}/support/tickets`;

function shops(role: string) {
  return (c: Call) =>
    c.method === 'GET' && c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
      : undefined;
}

function ticket(over: Record<string, unknown> = {}) {
  return {
    id: 't1',
    number: 'SUP-000007',
    subject: 'Payout not received',
    category: 'payouts',
    status: 'open',
    storeId: 's1',
    storeName: 'Indiranagar',
    orderId: null,
    orderNumber: null,
    lastMessageAt: '2026-10-24T10:00:00.000Z',
    createdAt: '2026-10-24T09:00:00.000Z',
    ...over,
  };
}

function thread(over: Record<string, unknown> = {}) {
  return {
    ticket: ticket(over),
    messages: [
      { id: 'm1', from: 'business', authorName: 'Lakshmi', body: 'The payout for last week did not arrive.', createdAt: '2026-10-24T09:00:00.000Z', attachments: [] },
      {
        id: 'm2',
        from: 'support',
        authorName: null,
        body: 'We are checking with the bank.',
        createdAt: '2026-10-24T10:00:00.000Z',
        attachments: [{ id: 'a9', fileName: 'utr.pdf', contentType: 'application/pdf', sizeBytes: 2048, url: 'https://s3.test/get?sig=y' }],
      },
    ],
    serverNow: '2026-10-24T11:00:00.000Z',
  };
}

function list(items: unknown[]): Handler {
  return (c) => (c.method === 'GET' && c.path === TICKETS ? json(200, { items, nextCursor: null }) : undefined);
}

function threadAt(body: unknown): Handler {
  return (c) => (c.method === 'GET' && c.path === `${TICKETS}/t1/messages` ? json(200, body) : undefined);
}

const ORDER = { id: 'o1', number: 'ORD-1042', customerName: 'Anil', totalPaise: 45000, placedAt: '2026-10-24T08:00:00.000Z', status: 'done' };

function orders(): Handler {
  return (c) => {
    if (c.method !== 'GET' || c.path !== `${BIZ}/stores/s1/orders`) return undefined;
    const status = new URLSearchParams(c.search).get('status');
    return json(200, { items: status === 'done' ? [ORDER] : [], nextCursor: null });
  };
}

let globalFetch: ReturnType<typeof vi.fn>;
beforeEach(() => {
  globalFetch = vi.fn(() => Promise.resolve(new Response(null, { status: 200 })));
  vi.stubGlobal('fetch', globalFetch);
});
afterEach(() => vi.unstubAllGlobals());

describe('B-15 help by role', () => {
  it('a cashier sees the guide and is told to ask a manager, with no tickets tab and no ticket calls', async () => {
    const { calls } = renderAt('/help', { messages, handlers: [shops('cashier'), list([])] });
    expect(await screen.findByText('Tickets are opened by the owner or a manager. Ask one of them to contact support for you.')).toBeInTheDocument();
    expect(screen.getByText('How TodayZ works for your shop')).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Tickets' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'New ticket' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path.startsWith(`${BIZ}/support`))).toBe(false);
  });

  it('a cashier opening a ticket link is told the same and nothing is fetched', async () => {
    const { calls } = renderAt('/help/t1', { messages, handlers: [shops('cashier'), threadAt(thread())] });
    expect(await screen.findByText(/Tickets are opened by the owner or a manager/)).toBeInTheDocument();
    expect(calls.some((c) => c.path.startsWith(`${BIZ}/support`))).toBe(false);
  });

  it('an owner gets the tickets tab with an empty state', async () => {
    renderAt('/help?tab=tickets', { messages, handlers: [shops('owner'), list([])] });
    expect(await screen.findByText('No tickets yet')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Tickets' })).toHaveAttribute('aria-selected', 'true');
  });

  it('a manager sees the list with status, store and a link to the thread', async () => {
    renderAt('/help?tab=tickets', { messages, handlers: [shops('manager'), list([ticket({ status: 'waiting' })])] });
    const row = await screen.findByRole('link', { name: /SUP-000007/ });
    expect(row).toHaveAttribute('href', '/help/t1');
    expect(within(row).getByText('Replied')).toBeInTheDocument();
    expect(within(row).getByText('Payout not received')).toBeInTheDocument();
    expect(within(row).getByText(/Indiranagar/)).toBeInTheDocument();
  });
});

describe('B-15 new ticket', () => {
  it('checks the form before calling the API', async () => {
    const { calls } = renderAt('/help/new', { messages, handlers: [shops('owner'), orders()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Send to support' }));
    expect(await screen.findByText('Choose what it is about.')).toBeInTheDocument();
    expect(screen.getByText('Write a subject of 3 to 120 characters.')).toBeInTheDocument();
    expect(screen.getByText('Write a message.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('opens the ticket with an Idempotency-Key, the store and the picked order, then shows the thread', async () => {
    const { calls, history } = renderAt('/help/new', {
      messages,
      handlers: [
        shops('owner'),
        orders(),
        (c) => (c.method === 'POST' && c.path === TICKETS ? json(201, thread({ orderId: 'o1', orderNumber: 'ORD-1042' })) : undefined),
        threadAt(thread({ orderId: 'o1', orderNumber: 'ORD-1042' })),
      ],
    });
    fireEvent.change(await screen.findByLabelText('What is it about?'), { target: { value: 'payouts' } });
    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: '  Payout not received ' } });
    expect(screen.getByLabelText('Which shop?')).toHaveValue('s1');
    const order = screen.getByLabelText('Which order?');
    await waitFor(() => expect(within(order).getByRole('option', { name: /ORD-1042/ })).toBeInTheDocument());
    fireEvent.change(order, { target: { value: 'o1' } });
    fireEvent.change(screen.getByLabelText('What happened?'), { target: { value: 'The payout for last week did not arrive.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));

    expect(await screen.findByRole('heading', { name: 'Ticket SUP-000007' })).toBeInTheDocument();
    const post = calls.find((c) => c.method === 'POST' && c.path === TICKETS);
    expect(post?.body).toEqual({ category: 'payouts', subject: 'Payout not received', body: 'The payout for last week did not arrive.', storeId: 's1', orderId: 'o1' });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(history.location.pathname).toBe('/help/t1');
    const read = new Set(calls.filter((c) => c.path === `${BIZ}/stores/s1/orders`).map((c) => new URLSearchParams(c.search).get('status')));
    expect(['done', 'missed', 'accepted'].every((s) => read.has(s))).toBe(true);
  });

  it('shows the field the server refused', async () => {
    renderAt('/help/new', {
      messages,
      handlers: [
        shops('owner'),
        orders(),
        (c) =>
          c.method === 'POST' && c.path === TICKETS
            ? json(422, { type: 'about:blank#validation_failed', title: 'validation_failed', status: 422, code: 'validation_failed', fields: { '/orderId': 'not_found' } })
            : undefined,
      ],
    });
    fireEvent.change(await screen.findByLabelText('What is it about?'), { target: { value: 'order' } });
    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'Wrong item' } });
    fireEvent.change(screen.getByLabelText('What happened?'), { target: { value: 'The customer got the wrong size.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));
    expect(await screen.findByText('Some details were not accepted. Check the form and try again.')).toBeInTheDocument();
    expect(screen.getByText('This was not accepted. Choose again.')).toBeInTheDocument();
  });
});

describe('B-15 attachments', () => {
  const made = {
    attachment: { id: 'a1', fileName: 'photo.png', contentType: 'image/png', sizeBytes: 4, url: null },
    upload: { url: 'https://s3.test/put?sig=x', method: 'PUT', headers: { 'Content-Type': 'image/png', 'Content-Length': '4' }, expiresAt: '2026-10-24T11:15:00.000Z' },
  };
  const attach: Handler = (c) => (c.method === 'POST' && c.path === `${BIZ}/support/attachments` ? json(201, made) : undefined);
  const fileInput = (container: HTMLElement) => container.querySelector('input[type=file]') as HTMLInputElement;

  it('registers the file, PUTs it with only the signed Content-Type, and sends its id with the ticket', async () => {
    const { calls, container } = renderAt('/help/new', {
      messages,
      handlers: [shops('owner'), orders(), attach, (c) => (c.method === 'POST' && c.path === TICKETS ? json(201, thread()) : undefined), threadAt(thread())],
    });
    await screen.findByLabelText('What is it about?');
    fireEvent.change(fileInput(container), { target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'photo.png', { type: 'image/png' })] } });
    expect(await screen.findByText('photo.png')).toBeInTheDocument();
    const reg = calls.find((c) => c.method === 'POST' && c.path === `${BIZ}/support/attachments`);
    expect(reg?.body).toEqual({ fileName: 'photo.png', contentType: 'image/png', sizeBytes: 4 });
    const [url, init] = globalFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://s3.test/put?sig=x');
    expect(init.method).toBe('PUT');
    expect(init.headers).toEqual({ 'Content-Type': 'image/png' });

    fireEvent.change(screen.getByLabelText('What is it about?'), { target: { value: 'app' } });
    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'Screen is blank' } });
    fireEvent.change(screen.getByLabelText('What happened?'), { target: { value: 'See the photo.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));
    await screen.findByRole('heading', { name: 'Ticket SUP-000007' });
    expect(calls.find((c) => c.method === 'POST' && c.path === TICKETS)?.body).toMatchObject({ attachmentIds: ['a1'] });
  });

  it('refuses a wrong type or an empty file before calling the API', async () => {
    const { calls, container } = renderAt('/help/new', { messages, handlers: [shops('owner'), orders(), attach] });
    await screen.findByLabelText('What is it about?');
    fireEvent.change(fileInput(container), { target: { files: [new File(['x'], 'a.exe', { type: 'application/x-msdownload' })] } });
    expect(await screen.findByText('a.exe is not a photo or PDF.')).toBeInTheDocument();
    fireEvent.change(fileInput(container), { target: { files: [new File([], 'b.png', { type: 'image/png' })] } });
    expect(await screen.findByText('b.png is empty.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });
});

describe('B-15 ticket thread', () => {
  it('shows the thread oldest first, support as "TodayZ support", and its file as a link', async () => {
    renderAt('/help/t1', { messages, handlers: [shops('owner'), threadAt(thread({ status: 'waiting' }))] });
    expect(await screen.findByRole('heading', { name: 'Ticket SUP-000007' })).toBeInTheDocument();
    const items = within(screen.getByRole('list', { name: 'Messages' })).getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('Lakshmi');
    expect(items[1]).toHaveTextContent('TodayZ support');
    expect(within(items[1] as HTMLElement).getByRole('link', { name: 'utr.pdf' })).toHaveAttribute('href', 'https://s3.test/get?sig=y');
    expect(screen.getByText('Sending a reply opens the ticket again.')).toBeInTheDocument();
  });

  it('sends a reply with an Idempotency-Key and clears the box', async () => {
    const replied = thread();
    replied.messages.push({ id: 'm3', from: 'business', authorName: 'Lakshmi', body: 'Thanks, waiting.', createdAt: '2026-10-24T11:00:00.000Z', attachments: [] });
    const { calls } = renderAt('/help/t1', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'POST' && c.path === `${TICKETS}/t1/messages` ? json(201, replied) : undefined), threadAt(thread({ status: 'waiting' }))],
    });
    const box = await screen.findByLabelText('Your message');
    fireEvent.change(box, { target: { value: ' Thanks, waiting. ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('Thanks, waiting.')).toBeInTheDocument();
    const post = calls.find((c) => c.method === 'POST' && c.path === `${TICKETS}/t1/messages`);
    expect(post?.body).toEqual({ body: 'Thanks, waiting.' });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(screen.getByLabelText('Your message')).toHaveValue('');
  });

  it('a reply to a ticket closed meanwhile says so', async () => {
    renderAt('/help/t1', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'POST' && c.path === `${TICKETS}/t1/messages` ? problem(409, 'invalid_transition') : undefined), threadAt(thread())],
    });
    fireEvent.change(await screen.findByLabelText('Your message'), { target: { value: 'Any news?' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('This ticket is closed and takes no more messages. Open a new ticket.')).toBeInTheDocument();
  });

  it('a closed ticket has no reply box and offers a new ticket', async () => {
    renderAt('/help/t1', { messages, handlers: [shops('owner'), threadAt(thread({ status: 'closed' }))] });
    expect(await screen.findByRole('link', { name: 'Open a new ticket' })).toHaveAttribute('href', '/help/new');
    expect(screen.queryByLabelText('Your message')).not.toBeInTheDocument();
  });

  it("another business's ticket (404) shows not found", async () => {
    renderAt('/help/t1', { messages, handlers: [shops('owner')] });
    expect(await screen.findByText('Ticket not found')).toBeInTheDocument();
  });
});
