import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';
import { resetAlerts } from './alerts';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';
const SERVER_NOW = '2026-10-23T12:30:00.000Z';

function shops(role: string) {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function summary(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    number: `TZ-${id}`,
    customerName: 'Meera',
    fulfilment: 'pickup',
    status: 'placed',
    statusReason: null,
    itemCount: 2,
    totalPaise: 24500,
    acceptBy: '2026-10-23T12:34:30.000Z',
    holdUntil: null,
    placedAt: '2026-10-23T12:29:30.000Z',
    seen: false,
    version: 1,
    ...over,
  };
}

function full(id: string, over: Record<string, unknown> = {}) {
  return {
    ...summary(id),
    customer: { name: 'Meera', phoneMasked: '+91 98xxx xx210' },
    statusNote: null,
    lines: [
      { id: 'l1', storeItemId: 'i1', name: 'Amul Butter', variant: null, packSize: 100, unit: 'g', imageKey: null, unitPricePaise: 5800, quantityRequested: 2, quantityAccepted: null, lineTotalPaise: 11600, priceSource: 'base', available: 9 },
      { id: 'l2', storeItemId: 'i2', name: 'Toor dal', variant: null, packSize: 1, unit: 'kg', imageKey: null, unitPricePaise: 12900, quantityRequested: 1, quantityAccepted: null, lineTotalPaise: 12900, priceSource: 'base', available: 0 },
    ],
    subtotalPaise: 24500,
    discountPaise: 0,
    itemSubtotalPaise: 24500,
    itemsAfterOffersPaise: 24500,
    creditsUsed: 0,
    deliveryFeePaise: 0,
    paymentStatus: 'pay_at_store',
    paymentMethod: null,
    codeLocked: false,
    codeAttemptsLeft: 5,
    partialConfirmBy: null,
    extendedAt: null,
    acceptedAt: null,
    collectedAt: null,
    timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: '2026-10-23T12:29:30.000Z' }],
    serverNow: SERVER_NOW,
    ...over,
  };
}

/** The order with only its in-stock line (the default fixture's Toor dal is short). */
function inStock(id: string, over: Record<string, unknown> = {}) {
  const order = full(id, over);
  return { ...order, lines: order.lines.slice(0, 1) };
}

function listHandler(byTab: Record<string, unknown[]>): Handler {
  return (c) => {
    if (c.method !== 'GET' || c.path !== `${BASE}/orders`) return undefined;
    const status = new URLSearchParams(c.search).get('status') ?? 'new';
    return json(200, { items: byTab[status] ?? [], nextCursor: null, serverNow: SERVER_NOW });
  };
}

function orderHandler(getOrder: () => unknown, id = 'o1'): Handler {
  return (c) => (c.method === 'GET' && c.path === `${BASE}/orders/${id}` ? json(200, getOrder()) : undefined);
}

beforeEach(() => {
  resetAlerts();
  vi.useRealTimers();
  // The live stream uses the global fetch; the tests never open a real connection (it falls back to polling).
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
});
afterEach(() => vi.unstubAllGlobals());

describe('B-04 orders list', () => {
  it('shows the tabs, the New badge and the countdown from the server clock', async () => {
    const t0 = Date.now();
    renderAt('/orders', {
      messages,
      handlers: [shops('cashier'), listHandler({ new: [summary('o1'), summary('o2')] }), orderHandler(() => full('o1')), orderHandler(() => full('o2'), 'o2')],
    });
    const tabs = await screen.findByRole('tablist', { name: 'Order status' });
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent?.replace(/\d+$/, '').trim())).toEqual(['New', 'Accepted', 'Ready', 'Done', 'Missed', 'Asks']);
    expect(within(tabs).getByRole('tab', { name: /New/ })).toHaveTextContent('2');
    const timers = await screen.findAllByTestId('countdown');
    expect(timers).toHaveLength(2);
    // acceptBy is 4m30s after serverNow, whatever the device clock says.
    expect(timers[0]).toHaveTextContent(/4:[23]\d|5:00/);
    expect(Date.now() - t0).toBeLessThan(5000);
    expect(await screen.findAllByText('Toor dal · 1 kg × 1')).not.toHaveLength(0);
  });

  it('asks for a tap before it can play the alert sound', async () => {
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [] })] });
    expect(await screen.findByRole('button', { name: 'Allow sound' })).toBeInTheDocument();
  });

  it('shows an empty state when there is nothing new', async () => {
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [] })] });
    expect(await screen.findByText('No new orders')).toBeInTheDocument();
  });

  it('accept sends an Idempotency-Key and the card leaves the list', async () => {
    let accepted = false;
    const { calls } = renderAt('/orders', {
      messages,
      handlers: [
        shops('cashier'),
        (c) =>
          c.method === 'POST' && c.path === `${BASE}/orders/o1/accept`
            ? ((accepted = true), json(200, full('o1', { status: 'accepted' })))
            : undefined,
        (c) => (c.method === 'GET' && c.path === `${BASE}/orders` ? json(200, { items: accepted ? [] : [summary('o1')], nextCursor: null, serverNow: SERVER_NOW }) : undefined),
        orderHandler(() => inStock('o1')),
      ],
    });
    await screen.findByText('Amul Butter · 100 g × 2');
    fireEvent.click(await screen.findByRole('button', { name: 'Accept order TZ-o1' }));
    await waitFor(() => expect(screen.queryByRole('article', { name: 'TZ-o1' })).not.toBeInTheDocument());
    const post = calls.find((c) => c.path.endsWith('/accept'));
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('disables Accept while a line is short and says why; Reject still works', async () => {
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [summary('o1')] }), orderHandler(() => full('o1'))] });
    const hint = await screen.findByText('Not enough stock for one item. Reject the order, or update the stock.');
    const accept = screen.getByRole('button', { name: 'Accept order TZ-o1' });
    expect(accept).toBeDisabled();
    expect(accept).toHaveAttribute('aria-describedby', hint.id);
    expect(screen.getByRole('button', { name: 'Reject order TZ-o1' })).toBeEnabled();
  });

  it('reject needs a reason, and sends it with an Idempotency-Key', async () => {
    const { calls } = renderAt('/orders', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/reject` ? json(200, full('o1', { status: 'rejected' })) : undefined),
        listHandler({ new: [summary('o1')] }),
        orderHandler(() => full('o1')),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Reject order TZ-o1' }));
    const dialog = await screen.findByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Reject order' });
    expect(confirm).toBeDisabled();
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Out of stock' }));
    fireEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/reject'))).toBe(true));
    const post = calls.find((c) => c.path.endsWith('/reject'));
    expect(post?.body).toEqual({ reason: 'out_of_stock' });
    expect(post?.headers.get('idempotency-key')).toBeTruthy();
  });
});

describe('B-04 order detail', () => {
  const accepted = (over: Record<string, unknown> = {}) =>
    full('o1', {
      status: 'accepted',
      acceptedAt: '2026-10-23T12:31:00.000Z',
      holdUntil: '2026-10-23T14:30:00.000Z',
      timeline: [
        { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: '2026-10-23T12:29:30.000Z' },
        { fromStatus: 'placed', toStatus: 'accepted', actor: 'staff', reason: null, at: '2026-10-23T12:31:00.000Z' },
      ],
      ...over,
    });

  it('disables Accept on a new order with short lines, with the reason in a warning', async () => {
    const short = renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => full('o1'))] });
    await screen.findByRole('heading', { level: 1, name: 'Order TZ-o1' });
    expect(screen.getByRole('button', { name: 'Accept' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Accept' })).toHaveAccessibleDescription('Not enough stock for one item. Reject the order, or update the stock.');
    expect(screen.queryByText('Accepting part of an order is not available yet.')).not.toBeInTheDocument();
    short.unmount();

    renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => inStock('o1'))] });
    await screen.findByRole('heading', { level: 1, name: 'Order TZ-o1' });
    expect(screen.getByRole('button', { name: 'Accept' })).toBeEnabled();
    expect(screen.queryByText(/Not enough stock for/)).not.toBeInTheDocument();
  });

  it('shows the timeline, the customer and the masked phone', async () => {
    renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => accepted())] });
    expect(await screen.findByRole('heading', { level: 1, name: 'Order TZ-o1' })).toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'Order progress' })).toBeInTheDocument();
    expect(screen.getByText('Meera')).toBeInTheDocument();
    expect(screen.getByText('Phone +91 98xxx xx210')).toBeInTheDocument();
    expect(screen.getByText('Customer pays at the counter: cash or UPI.')).toBeInTheDocument();
  });

  it('hides Cancel from a cashier and shows it to a manager, where "other" needs a note', async () => {
    const cashier = renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => accepted())] });
    await screen.findByRole('heading', { level: 1, name: 'Order TZ-o1' });
    expect(screen.queryByRole('button', { name: 'Cancel order' })).not.toBeInTheDocument();
    cashier.unmount();

    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('manager'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/cancel` ? json(200, accepted({ status: 'cancelled' })) : undefined),
        orderHandler(() => accepted()),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel order' }));
    const dialog = await screen.findByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Cancel order' });
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Other reason' }));
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Power cut' } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/cancel'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/cancel'))?.body).toEqual({ reason: 'other', note: 'Power cut' });
  });

  async function typeCode(digits: string) {
    const boxes = await screen.findAllByRole('textbox', { name: /Digit|code/i });
    digits.split('').forEach((d, i) => {
      const box = boxes[i];
      if (box) fireEvent.change(box, { target: { value: d } });
    });
  }

  it('shows the tries left after a wrong code, and clears the boxes', async () => {
    renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) =>
          c.method === 'POST' && c.path === `${BASE}/orders/o1/verify-code`
            ? json(422, { type: 'about:blank#code_invalid', title: 'code_invalid', status: 422, code: 'code_invalid', extensions: { attemptsLeft: 3 } })
            : undefined,
        orderHandler(() => accepted()),
      ],
    });
    await typeCode('1234');
    fireEvent.click(screen.getByRole('radio', { name: 'Cash' }));
    fireEvent.click(screen.getByRole('button', { name: 'Hand over order' }));
    expect(await screen.findByText(/That code is not right. 3 tries left./)).toBeInTheDocument();
    for (const box of screen.getAllByRole('textbox', { name: /Digit|code/i })) expect(box).toHaveValue('');
  });

  it('never puts the code in a URL, a query key or storage', async () => {
    const { calls, queryClient } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/verify-code` ? json(200, accepted({ status: 'collected', paymentMethod: 'cash' })) : undefined),
        orderHandler(() => accepted()),
      ],
    });
    await typeCode('4821');
    fireEvent.click(screen.getByRole('radio', { name: 'Cash' }));
    fireEvent.click(screen.getByRole('button', { name: 'Hand over order' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/verify-code'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/verify-code'))?.body).toEqual({ code: '4821', paymentMethod: 'cash' });
    expect(calls.every((c) => !c.path.includes('4821'))).toBe(true);
    expect(JSON.stringify(queryClient.getQueryCache().getAll().map((q) => q.queryKey))).not.toContain('4821');
    expect(JSON.stringify(Object.entries(localStorage))).not.toContain('4821');
  });

  it('explains the lock and offers no code boxes', async () => {
    renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => accepted({ codeLocked: true, codeAttemptsLeft: 0 }))] });
    expect(await screen.findByText('Code entry is locked')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Hand over order' })).not.toBeInTheDocument();
  });

  it('marks a pickup order ready', async () => {
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/ready` ? json(200, accepted({ status: 'ready' })) : undefined),
        orderHandler(() => accepted()),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Mark ready' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/ready'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/ready'))?.headers.get('idempotency-key')).toBeTruthy();
  });

  it('says so when the order is not found', async () => {
    renderAt('/orders/missing', { messages, handlers: [shops('cashier')] });
    expect(await screen.findByText('This order was not found for this shop.')).toBeInTheDocument();
  });
});

describe('B-04 live updates', () => {
  it('refetches the lists when the store stream says an order was created', async () => {
    const enc = new TextEncoder();
    const stream: { push: ((t: string) => void) | null } = { push: null };
    vi.stubGlobal('fetch', (url: string) => {
      if (!String(url).endsWith('/stream')) return Promise.resolve(new Response(null, { status: 404 }));
      const body = new ReadableStream<Uint8Array>({
        start(c) {
          stream.push = (t) => c.enqueue(enc.encode(t));
        },
      });
      return Promise.resolve(new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } }));
    });
    let items = [summary('o1', { seen: true })];
    renderAt('/orders', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'GET' && c.path === `${BASE}/orders` ? json(200, { items, nextCursor: null, serverNow: SERVER_NOW }) : undefined),
        orderHandler(() => full('o1')),
        orderHandler(() => full('o2'), 'o2'),
      ],
    });
    expect(await screen.findByRole('article', { name: 'TZ-o1' })).toBeInTheDocument();
    await waitFor(() => expect(stream.push).not.toBeNull());
    items = [summary('o1', { seen: true }), summary('o2')];
    stream.push?.('event: order.created\nid: 2\ndata: {"type":"order.created","id":"o2","version":1}\n\n');
    expect(await screen.findByRole('article', { name: 'TZ-o2' })).toBeInTheDocument();
  });

  it('says so when the stream is down and the page polls instead', async () => {
    vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 503 })));
    vi.useFakeTimers();
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [] })] });
    // Three failed connections (3 s, then 6 s of backoff) switch the stream to polling.
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(screen.getByText(/Live updates are paused/)).toBeInTheDocument();
  });
});
