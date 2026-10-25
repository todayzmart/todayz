import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import type { TicketSummary } from '@todayz/api-client/gen/model/ticketSummary';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NOW } from '../test/fixtures';
import { adminMe, json, problem, renderAt, type Handler } from '../test/render';
import { minutesLeft } from './labels';
import { ticketsParams, validateSupportSearch } from './search';

const TID = '01JTICKET00000000000000T01';
const T2 = '01JTICKET00000000000000T02';
const BID = '01JB0BZ1A2B3C4D5E6F7G8H9J0';
const SID = '01JST0RE000000000000000S01';
const OWNER = '01JOWNER00000000000000OW01';
const PRIYA = '01JAGENT00000000000000AG01';
const ME = adminMe(['support']).id;
const KEY = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const base = `/v1/admin/tickets/${TID}`;
const at = (minutes: number) => new Date(Date.parse(NOW) + minutes * 60_000).toISOString();

function summary(over: Partial<TicketSummary> = {}): TicketSummary {
  return {
    id: TID,
    number: 'SUP-001001',
    subject: 'Last week’s payout has not arrived',
    category: 'payouts',
    status: 'open',
    priority: 'normal',
    openerType: 'business',
    opener: { id: OWNER, name: 'Lakshmi' },
    businessId: BID,
    businessName: 'Lakshmi Traders',
    storeId: SID,
    storeName: 'Lakshmi Indiranagar',
    orderId: null,
    orderNumber: null,
    assignee: null,
    slaDueAt: at(45),
    overdue: false,
    lastMessageAt: at(-15),
    createdAt: at(-15),
    ...over,
  };
}

function detail(over: Partial<TicketDetail> = {}, t: Partial<TicketSummary> = {}): TicketDetail {
  return {
    ticket: summary(t),
    order: null,
    firstResponseAt: null,
    resolvedAt: null,
    closedAt: null,
    closeReason: null,
    messages: [
      {
        id: '01JMSG0000000000000000M001',
        kind: 'reply',
        authorType: 'business',
        author: { id: OWNER, name: 'Lakshmi' },
        body: 'The payout shows as sent but it is not in our bank.',
        createdAt: at(-15),
        attachments: [{ id: '01JATT0000000000000000A001', fileName: 'statement.pdf', contentType: 'application/pdf', sizeBytes: 2048, url: 'https://s3.test/a' }],
      },
      {
        id: '01JMSG0000000000000000M002',
        kind: 'note',
        authorType: 'admin',
        author: { id: PRIYA, name: 'Priya' },
        body: 'Bank batch went out on Monday; checking the UTR.',
        createdAt: at(-5),
        attachments: [],
      },
    ],
    agents: [
      { id: PRIYA, name: 'Priya' },
      { id: ME, name: 'Asha Rao' },
    ],
    assistEnabled: false,
    serverNow: NOW,
    ...over,
  };
}

function api(d: TicketDetail = detail()): Handler {
  let current = d;
  return (c) => {
    if (c.path === '/v1/admin/tickets' && c.method === 'GET') {
      return json(200, {
        items: [summary(), summary({ id: T2, number: 'SUP-001002', subject: 'My pickup was not ready', openerType: 'customer', opener: { id: OWNER, name: 'Asha' }, businessId: null, businessName: null, priority: 'urgent', slaDueAt: at(-20), overdue: true })],
        counts: { open: 2, waiting: 1, resolved: 4, closed: 9, overdue: 1 },
        nextCursor: null,
        serverNow: NOW,
      });
    }
    if (c.path === base && c.method === 'GET') return json(200, current);
    if (c.path === `${base}/reply` || c.path === `${base}/notes`) {
      const body = c.body as { body: string; attachmentIds?: string[] };
      const kind = c.path.endsWith('reply') ? 'reply' : 'note';
      current = {
        ...current,
        ticket: kind === 'reply' ? { ...current.ticket, status: 'waiting', slaDueAt: null } : current.ticket,
        messages: [
          ...current.messages,
          { id: '01JMSG0000000000000000M009', kind, authorType: 'admin', author: { id: ME, name: 'Asha Rao' }, body: body.body, createdAt: NOW, attachments: [] },
        ],
      };
      return json(200, current);
    }
    if (c.path === `${base}/assign`) {
      const id = (c.body as { assigneeId: string | null }).assigneeId;
      current = { ...current, ticket: { ...current.ticket, assignee: id ? current.agents.find((a) => a.id === id) ?? null : null } };
      return json(200, current);
    }
    if (c.path === `${base}/priority`) {
      const { priority } = c.body as { priority: 'normal' | 'high' | 'urgent' };
      current = { ...current, ticket: { ...current.ticket, priority } };
      return json(200, current);
    }
    if (c.path === `${base}/close`) {
      const b = c.body as { outcome: 'resolved' | 'closed'; reason: string };
      current = { ...current, ticket: { ...current.ticket, status: b.outcome, slaDueAt: null }, closeReason: b.reason };
      return json(200, current);
    }
    if (c.path === `${base}/attachments`) {
      const b = c.body as { fileName: string; contentType: string; sizeBytes: number };
      return json(201, {
        attachment: { id: '01JATT0000000000000000A009', fileName: b.fileName, contentType: b.contentType, sizeBytes: b.sizeBytes, url: null },
        upload: { url: 'https://s3.test/put', method: 'PUT', headers: { 'Content-Type': b.contentType, 'Content-Length': String(b.sizeBytes) }, expiresAt: at(15) },
      });
    }
    return undefined;
  };
}

afterEach(() => vi.unstubAllGlobals());

describe('support search', () => {
  it('keeps only known filters and leaves open as the default', () => {
    expect(validateSupportSearch({ status: 'open', assignee: 'me', opener: 'robot' })).toEqual({ assignee: 'me' });
    expect(validateSupportSearch({ status: 'closed', opener: 'business' })).toEqual({ status: 'closed', opener: 'business' });
    expect(ticketsParams({})).toEqual({ status: 'open' });
  });

  it('counts minutes from the server clock', () => {
    expect(minutesLeft({ slaDueAt: at(45) }, NOW)).toBe(45);
    expect(minutesLeft({ slaDueAt: at(-20) }, NOW)).toBe(-20);
    expect(minutesLeft({ slaDueAt: null }, NOW)).toBeNull();
  });
});

describe('A-14 inbox', () => {
  it('shows counts per status and the SLA chip from serverNow', async () => {
    renderAt('/support', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Tickets' });
    expect(await within(table).findByRole('link', { name: /SUP-001001/ })).toHaveAttribute('href', `/support/${TID}`);
    expect(within(table).getByText('Due in 45 min')).toBeInTheDocument();
    expect(within(table).getByText('Overdue by 20 min')).toBeInTheDocument();
    expect(within(table).getByText('Lakshmi · Lakshmi Traders')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Closed/ })).toHaveTextContent('9');
    expect(screen.getByText('1 open ticket is past its reply time.')).toBeInTheDocument();
  });

  it('keeps the filters in the URL and sends them', async () => {
    const { calls, router } = renderAt('/support', { roles: ['support'], handlers: [api()] });
    await screen.findByRole('table', { name: 'Tickets' });
    fireEvent.click(screen.getByRole('radio', { name: 'Mine' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ assignee: 'me' }));
    fireEvent.click(screen.getByRole('tab', { name: /Waiting/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ assignee: 'me', status: 'waiting' }));
    await waitFor(() => {
      const last = calls.filter((c) => c.path === '/v1/admin/tickets').at(-1);
      const q = new URLSearchParams(last?.search);
      expect(q.get('status')).toBe('waiting');
      expect(q.get('assignee')).toBe('me');
    });
  });
});

describe('A-14 ticket', () => {
  it('shows the thread with notes marked, the attachment and no assist slot when off', async () => {
    const { container } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    expect(await screen.findByRole('heading', { level: 1, name: 'Ticket SUP-001001' })).toBeInTheDocument();
    const note = container.querySelector('[data-kind="note"]');
    expect(note).not.toBeNull();
    expect(within(note as HTMLElement).getByText('Internal note')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'statement.pdf' })).toHaveAttribute('href', 'https://s3.test/a');
    expect(container.querySelector('[data-slot="support-assist"]')).toBeNull();
  });

  it('mounts the assist slot only while triage assist is on', async () => {
    const { container } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api(detail({ assistEnabled: true }))] });
    await screen.findByRole('heading', { level: 1, name: 'Ticket SUP-001001' });
    expect(container.querySelector(`[data-slot="support-assist"][data-ticket="${TID}"]`)).not.toBeNull();
  });

  it('replies with an Idempotency-Key and the ticket moves to waiting', async () => {
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('Write a message, up to 4,000 characters.')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Reply to the opener'), { target: { value: '  The UTR is on its way.  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    await waitFor(() => expect(screen.getByText('The UTR is on its way.')).toBeInTheDocument());
    const post = calls.find((c) => c.path === `${base}/reply`);
    expect(post?.body).toEqual({ body: 'The UTR is on its way.' });
    expect(post?.headers.get('Idempotency-Key')).toMatch(KEY);
    expect(await screen.findAllByText('Waiting')).not.toHaveLength(0);
    // The reply clock stops while the opener has the turn.
    expect(screen.getByText('Paused until the opener replies')).toBeInTheDocument();
  });

  it('adds an internal note to the notes endpoint', async () => {
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('radio', { name: 'Internal note' }));
    fireEvent.change(screen.getByLabelText('Internal note', { selector: 'textarea' }), { target: { value: 'Called the bank.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add note' }));
    await waitFor(() => expect(calls.some((c) => c.path === `${base}/notes`)).toBe(true));
    expect(calls.some((c) => c.path === `${base}/reply`)).toBe(false);
  });

  it('uploads a file to the presigned URL and the reply claims it', async () => {
    const put = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', put);
    const { calls, container } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    await screen.findByRole('heading', { level: 1, name: 'Ticket SUP-001001' });
    const input = container.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error('no file input');
    const bad = new File(['x'], 'notes.txt', { type: 'text/plain' });
    fireEvent.change(input, { target: { files: [bad] } });
    expect(await screen.findByText('notes.txt is not a JPEG, PNG, WebP or PDF.')).toBeInTheDocument();
    const file = new File(['0123456789'], 'receipt.png', { type: 'image/png' });
    fireEvent.change(input, { target: { files: [file] } });
    expect(await screen.findByRole('button', { name: 'Remove receipt.png' })).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/attachments`)?.body).toEqual({ fileName: 'receipt.png', contentType: 'image/png', sizeBytes: 10 });
    expect(put).toHaveBeenCalledWith('https://s3.test/put', expect.objectContaining({ method: 'PUT', headers: { 'Content-Type': 'image/png' } }));
    fireEvent.change(screen.getByLabelText('Reply to the opener'), { target: { value: 'See the receipt.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    await waitFor(() => expect(calls.find((c) => c.path === `${base}/reply`)?.body).toEqual({ body: 'See the receipt.', attachmentIds: ['01JATT0000000000000000A009'] }));
  });

  it('assigns to me and to a chosen agent', async () => {
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Assign to me' }));
    expect(await screen.findByText('Assigned to Asha Rao')).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/assign`)?.body).toEqual({ assigneeId: ME });
    fireEvent.change(screen.getByLabelText('Agent'), { target: { value: PRIYA } });
    fireEvent.click(screen.getByRole('button', { name: 'Assign' }));
    expect(await screen.findByText('Assigned to Priya')).toBeInTheDocument();
  });

  it('changes the priority and refuses the same one', async () => {
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    const pick = await screen.findByLabelText('Set priority');
    const change = screen.getByRole('button', { name: 'Change' });
    expect(change).toBeDisabled();
    fireEvent.change(pick, { target: { value: 'urgent' } });
    fireEvent.click(change);
    expect(await screen.findByText('Priority set to Urgent')).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/priority`)?.body).toEqual({ priority: 'urgent' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Change' })).toBeDisabled());
  });

  it('resolves only with a reason', async () => {
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Resolve' }));
    const dialog = await screen.findByRole('dialog', { name: 'Resolve SUP-001001?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Resolve' }));
    expect(calls.some((c) => c.path === `${base}/close`)).toBe(false);
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'UTR shared' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Resolve' }));
    await waitFor(() => expect(calls.some((c) => c.path === `${base}/close`)).toBe(true));
    expect(await screen.findByText('SUP-001001 resolved')).toBeInTheDocument();
    expect(await screen.findByText('None')).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/close`)?.body).toEqual({ outcome: 'resolved', reason: 'UTR shared' });
  });

  it('takes only notes on a closed ticket', async () => {
    renderAt(`/support/${TID}`, { roles: ['support'], handlers: [api(detail({ closedAt: NOW, closeReason: 'Duplicate' }, { status: 'closed', slaDueAt: null }))] });
    await screen.findByRole('heading', { level: 1, name: 'Ticket SUP-001001' });
    expect(screen.queryByRole('button', { name: 'Send reply' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Close' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add note' })).toBeInTheDocument();
    expect(screen.getByLabelText('Agent')).toBeDisabled();
    expect(screen.getByLabelText('Set priority')).toBeDisabled();
  });

  it('says when the ticket is not there', async () => {
    renderAt(`/support/${TID}`, { roles: ['support'], handlers: [(c) => (c.path === base ? problem(404, 'not_found') : undefined)] });
    expect(await screen.findByText('This ticket does not exist.')).toBeInTheDocument();
  });
});
