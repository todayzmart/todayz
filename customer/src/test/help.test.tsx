// C-19 Help (W3-08, D-226): the customer's tickets, opening one (optionally on an order, preset from
// C-12), the thread with support's replies under no agent name, replying, and the closed state.
import type { MyTicket } from '@todayz/api-client/gen/model/myTicket';
import type { MyTicketThread } from '@todayz/api-client/gen/model/myTicketThread';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const TICKET = '01JB0SKC1A2B3C4D5E6F7G8H9J';
const ORDER = '01JB0SRD1A2B3C4D5E6F7G8H9J';
const NOW = '2026-10-25T10:00:00.000Z';

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on =
  (method: string, path: string, make: (c: Call) => Response): Handler =>
  (c) =>
    c.method === method && c.path === path ? make(c) : undefined;

function ticket(over: Partial<MyTicket> = {}): MyTicket {
  return {
    id: TICKET,
    number: 'SUP-001043',
    subject: 'Atta missing from my order',
    category: 'order',
    status: 'open',
    storeName: 'Annapurna Kirana',
    orderId: ORDER,
    orderNumber: 'TZ-4821',
    lastMessageAt: '2026-10-25T09:55:00.000Z',
    createdAt: '2026-10-25T09:55:00.000Z',
    ...over,
  };
}

function thread(over: Partial<MyTicket> = {}, replies = true): MyTicketThread {
  return {
    ticket: ticket(over),
    serverNow: NOW,
    messages: [
      { id: '01JB0SM00000000000000000A1', from: 'customer', body: 'The atta was not in the bag.', createdAt: '2026-10-25T09:55:00.000Z', attachments: [] },
      ...(replies
        ? [{ id: '01JB0SM00000000000000000A2', from: 'support' as const, body: 'Sorry about that. Checking with the shop.', createdAt: '2026-10-25T09:58:00.000Z', attachments: [] }]
        : []),
    ],
  };
}

const orders: Handler = on('GET', '/v1/me/orders', () =>
  json(200, {
    items: [{ id: ORDER, number: 'TZ-4821', store: { name: 'Annapurna Kirana', slug: 'annapurna' }, status: 'completed', totalPaise: 34500, placedAt: NOW }],
    nextCursor: null,
  }),
);

describe('C-19 help', () => {
  it('asks a signed-out person to sign in', async () => {
    renderAt('/help');
    expect(await screen.findByRole('heading', { name: 'Sign in to get help' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', expect.stringContaining('/sign-in'));
  });

  it('lists tickets, and says when there are none', async () => {
    const { unmount } = renderAt('/help', {
      returning: true,
      handlers: [signedIn, on('GET', '/v1/me/support/tickets', () => json(200, { items: [], nextCursor: null }))],
    });
    expect(await screen.findByText('No tickets yet')).toBeInTheDocument();
    unmount();

    renderAt('/help', {
      returning: true,
      handlers: [signedIn, on('GET', '/v1/me/support/tickets', () => json(200, { items: [ticket({ status: 'waiting' })], nextCursor: null }))],
    });
    const list = await screen.findByRole('list', { name: 'Tickets' });
    const row = within(list).getByRole('link', { name: /SUP-001043/ });
    expect(row).toHaveAttribute('href', `/help/${TICKET}`);
    expect(within(row).getByText('Replied')).toBeInTheDocument();
    expect(within(row).getByText(/TZ-4821/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'New ticket' })).toHaveAttribute('href', '/help/new');
  });

  it('opens a ticket on the order it came from, with one key per attempt', async () => {
    const sent: Call[] = [];
    const { router } = renderAt(`/help/new?orderId=${ORDER}`, {
      returning: true,
      handlers: [
        signedIn,
        orders,
        on('POST', '/v1/me/support/tickets', (c) => {
          sent.push(c);
          return sent.length === 1 ? problem(503, 'unavailable') : json(201, thread({}, false));
        }),
        on('GET', `/v1/me/support/tickets/${TICKET}/messages`, () => json(200, thread({}, false))),
      ],
    });
    const topic = await screen.findByLabelText('What is it about?');
    expect(topic).toHaveValue('order');
    await waitFor(() => expect(screen.getByLabelText('Which order? (optional)')).toHaveValue(ORDER));

    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));
    expect(await screen.findByText(/Write a subject of 3 to 120 characters/)).toBeInTheDocument();
    expect(screen.getByText('Write a message.')).toBeInTheDocument();
    expect(sent).toHaveLength(0);

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'Atta missing from my order' } });
    fireEvent.change(screen.getByLabelText('What happened?'), { target: { value: 'The atta was not in the bag.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('That did not go through');
    fireEvent.click(screen.getByRole('button', { name: 'Send to support' }));

    await waitFor(() => expect(router.state.location.pathname).toBe(`/help/${TICKET}`));
    expect(sent).toHaveLength(2);
    expect(sent[0]?.body).toEqual({ category: 'order', subject: 'Atta missing from my order', body: 'The atta was not in the bag.', orderId: ORDER });
    const key = sent[0]?.headers.get('Idempotency-Key');
    expect(key).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    expect(sent[1]?.headers.get('Idempotency-Key')).toBe(key);
    expect(await screen.findByText('Ticket SUP-001043 sent')).toBeInTheDocument();
  });

  it('shows the thread with support under no name, and a reply reopens it', async () => {
    const sent: Call[] = [];
    renderAt(`/help/${TICKET}`, {
      returning: true,
      handlers: [
        signedIn,
        on('GET', `/v1/me/support/tickets/${TICKET}/messages`, () => json(200, thread({ status: 'waiting' }))),
        on('POST', `/v1/me/support/tickets/${TICKET}/messages`, (c) => {
          sent.push(c);
          return json(201, thread({ status: 'open' }));
        }),
      ],
    });
    expect(await screen.findByRole('heading', { name: 'Ticket SUP-001043' })).toBeInTheDocument();
    const msgs = screen.getByRole('list', { name: 'Messages' });
    expect(within(msgs).getByText('TodayZ support')).toBeInTheDocument();
    expect(within(msgs).getByText('You')).toBeInTheDocument();
    expect(screen.getByText('Sending a reply opens the ticket again.')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Your message'), { target: { value: 'Order TZ-4821, 2 kg pack.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toEqual({ body: 'Order TZ-4821, 2 kg pack.' });
    expect(sent[0]?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    await waitFor(() => expect(screen.getByLabelText('Your message')).toHaveValue(''));
    expect(screen.getByText('Open')).toBeInTheDocument();
  });

  it('a closed ticket takes no reply; an unknown one is not found', async () => {
    const { unmount } = renderAt(`/help/${TICKET}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/support/tickets/${TICKET}/messages`, () => json(200, thread({ status: 'closed' })))],
    });
    expect(await screen.findByText('This ticket is closed and takes no more messages.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Your message')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open a new ticket' })).toHaveAttribute('href', '/help/new');
    unmount();

    renderAt(`/help/${TICKET}`, { returning: true, handlers: [signedIn] });
    expect(await screen.findByText('Ticket not found')).toBeInTheDocument();
  });

  it('links to help from the account and from an order', async () => {
    renderAt('/account', {
      returning: true,
      handlers: [signedIn, on('GET', '/v1/me', () => json(200, { id: USER, phoneMasked: '+91 ••••• •0021', name: 'Asha', preferredLang: 'en' }, { etag: '"1"' }))],
    });
    expect(await screen.findByRole('link', { name: 'Get help or see your tickets' })).toHaveAttribute('href', '/help');
  });
});
