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

async function typeCode(digits: string) {
  const boxes = await screen.findAllByRole('textbox', { name: /Digit|code/i });
  digits.split('').forEach((d, i) => {
    const box = boxes[i];
    if (box) fireEvent.change(box, { target: { value: d } });
  });
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
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent?.replace(/\d+$/, '').trim())).toEqual(['New', 'Accepted', 'Ready', 'Out for delivery', 'Returns to confirm', 'Done', 'Missed', 'Asks']);
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

  it('disables Accept while a line is short and offers Accept part; Reject still works', async () => {
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [summary('o1')] }), orderHandler(() => full('o1'))] });
    const hint = await screen.findByText('Not enough stock for one item. Accept part, reject the order, or update the stock.');
    const accept = screen.getByRole('button', { name: 'Accept order TZ-o1' });
    expect(accept).toBeDisabled();
    expect(accept).toHaveAttribute('aria-describedby', hint.id);
    expect(screen.getByRole('button', { name: 'Accept part of order TZ-o1' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Reject order TZ-o1' })).toBeEnabled();
  });

  it('keeps the reject-or-restock hint and disables Accept part when the stock covers nothing', async () => {
    const empty = () => {
      const o = full('o1');
      return { ...o, lines: o.lines.map((l) => ({ ...l, available: 0 })) };
    };
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [summary('o1')] }), orderHandler(empty)] });
    const hint = await screen.findByText('Not enough stock for 2 items. Reject the order, or update the stock.');
    const part = screen.getByRole('button', { name: 'Accept part of order TZ-o1' });
    expect(part).toBeDisabled();
    expect(part).toHaveAttribute('aria-describedby', hint.id);
  });

  it('accept part prefills what the stock covers and sends every line with an Idempotency-Key', async () => {
    const { calls } = renderAt('/orders', {
      messages,
      handlers: [
        shops('cashier'),
        (c) =>
          c.method === 'POST' && c.path === `${BASE}/orders/o1/accept`
            ? json(200, full('o1', { status: 'awaiting_customer', partialConfirmBy: '2026-10-23T12:35:30.000Z' }))
            : undefined,
        listHandler({ new: [summary('o1')] }),
        orderHandler(() => full('o1')),
      ],
    });
    const part = await screen.findByRole('button', { name: 'Accept part of order TZ-o1' });
    await waitFor(() => expect(part).toBeEnabled());
    fireEvent.click(part);
    const dialog = await screen.findByRole('dialog', { name: 'Accept part of TZ-o1' });
    expect(within(dialog).getByText('Asked 2 · 9 in stock')).toBeInTheDocument();
    expect(within(dialog).getByText('Asked 1 · out of stock')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Send to customer' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/accept'))).toBe(true));
    const post = calls.find((c) => c.path.endsWith('/accept'));
    expect(post?.body).toEqual({ lines: [{ orderItemId: 'l1', quantity: 2 }, { orderItemId: 'l2', quantity: 0 }] });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Accept part of TZ-o1' })).not.toBeInTheDocument());
  });

  it('accept part needs a lowered quantity and refuses giving nothing', async () => {
    renderAt('/orders', { messages, handlers: [shops('cashier'), listHandler({ new: [summary('o1')] }), orderHandler(() => inStock('o1'))] });
    const part = await screen.findByRole('button', { name: 'Accept part of order TZ-o1' });
    await waitFor(() => expect(part).toBeEnabled());
    fireEvent.click(part);
    const dialog = await screen.findByRole('dialog', { name: 'Accept part of TZ-o1' });
    const send = within(dialog).getByRole('button', { name: 'Send to customer' });
    expect(send).toBeDisabled();
    expect(send).toHaveAccessibleDescription('Lower a quantity to accept part, or accept the whole order.');
    const less = within(dialog).getByRole('button', { name: /One less/ });
    fireEvent.click(less);
    expect(send).toBeEnabled();
    fireEvent.click(less);
    expect(send).toBeDisabled();
    expect(send).toHaveAccessibleDescription('Keep at least one item. To give nothing, reject the order.');
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
    expect(screen.getByRole('button', { name: 'Accept' })).toHaveAccessibleDescription('Not enough stock for one item. Accept part, reject the order, or update the stock.');
    expect(screen.getByRole('button', { name: 'Accept part' })).toBeEnabled();
    short.unmount();

    renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => inStock('o1'))] });
    await screen.findByRole('heading', { level: 1, name: 'Order TZ-o1' });
    expect(screen.getByRole('button', { name: 'Accept' })).toBeEnabled();
    expect(screen.queryByText(/Not enough stock for/)).not.toBeInTheDocument();
  });

  it('shows the changed quantities while the customer decides', async () => {
    const waiting = () => {
      const o = full('o1', { status: 'awaiting_customer', partialConfirmBy: '2026-10-23T12:35:30.000Z' });
      return { ...o, lines: [{ ...o.lines[0], quantityAccepted: 1, lineTotalPaise: 5800 }, { ...o.lines[1], quantityAccepted: 0, lineTotalPaise: 0 }] };
    };
    renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(waiting)] });
    expect(await screen.findByText('Waiting for Meera to take the changed order or cancel it.')).toBeInTheDocument();
    expect(screen.getByText('Amul Butter · 100 g × 1 (asked 2)')).toBeInTheDocument();
    expect(screen.getByText('Toor dal · 1 kg × 0 (asked 1)')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept part' })).not.toBeInTheDocument();
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

describe('B-04 delivery (BK-24–27)', () => {
  const ADDRESS = { label: 'Home', line1: '12 3rd Cross', line2: 'HAL 2nd Stage', landmark: 'Opp. temple', pincode: '560038', lat: 12.97, lng: 77.64 };
  const delivery = (over: Record<string, unknown> = {}) =>
    inStock('o1', {
      fulfilment: 'delivery',
      status: 'accepted',
      acceptedAt: '2026-10-23T12:31:00.000Z',
      dispatchBy: '2026-10-23T13:01:00.000Z',
      deliveryAddress: ADDRESS,
      deliveryFeePaise: 2500,
      deliveryFailedAt: null,
      returnOutcome: null,
      returnConfirmedAt: null,
      ...over,
    });

  it('dispatches an accepted delivery order, with the deadline and no Mark ready or code', async () => {
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/dispatch` ? json(200, delivery({ status: 'out_for_delivery' })) : undefined),
        orderHandler(() => delivery()),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Dispatch' }));
    expect(screen.getByText(/Send it out by .*, or the order expires\./)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mark ready' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Mark delivered' })).not.toBeInTheDocument();
    expect(screen.getByLabelText('Delivery address')).toHaveTextContent('12 3rd Cross, HAL 2nd Stage');
    expect(screen.getByText('Customer pays at the door: cash or UPI.')).toBeInTheDocument();
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/dispatch'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/dispatch'))?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('shows the full phone only while out for delivery, and delivers with the code', async () => {
    const masked = renderAt('/orders/o1', { messages, handlers: [shops('cashier'), orderHandler(() => delivery())] });
    expect(await screen.findByText('Phone +91 98xxx xx210')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Call/ })).not.toBeInTheDocument();
    masked.unmount();

    const out = () => delivery({ status: 'out_for_delivery', customer: { name: 'Meera', phoneMasked: '+91 98xxx xx210', phone: '+919876543210' } });
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/verify-code` ? json(200, delivery({ status: 'delivered', paymentMethod: 'upi' })) : undefined),
        orderHandler(out),
      ],
    });
    expect(await screen.findByRole('link', { name: 'Call +919876543210' })).toHaveAttribute('href', 'tel:+919876543210');
    expect(screen.getByRole('heading', { name: "Deliver with the customer's code" })).toBeInTheDocument();
    await typeCode('7315');
    fireEvent.click(screen.getByRole('radio', { name: 'UPI' }));
    fireEvent.click(screen.getByRole('button', { name: 'Mark delivered' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/verify-code'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/verify-code'))?.body).toEqual({ code: '7315', paymentMethod: 'upi' });
  });

  it('marks a delivery failed with a reason, where "other" needs a note', async () => {
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/fail-delivery` ? json(200, delivery({ status: 'delivery_failed', deliveryFailedAt: SERVER_NOW })) : undefined),
        orderHandler(() => delivery({ status: 'out_for_delivery' })),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Delivery failed' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getAllByRole('radio').map((r) => r.getAttribute('aria-label') ?? r.closest('label')?.textContent)).toEqual([
      'Customer not reachable',
      'Address not found',
      'Customer refused the order',
      'Other reason',
    ]);
    const confirm = within(dialog).getByRole('button', { name: 'Mark delivery failed' });
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Other reason' }));
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Gate locked' } });
    fireEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/fail-delivery'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/fail-delivery'))?.body).toEqual({ reason: 'other', note: 'Gate locked' });
  });

  const failed = (over: Record<string, unknown> = {}) =>
    delivery({ status: 'delivery_failed', statusReason: 'customer_unreachable', deliveryFailedAt: new Date(Date.now() - (2 * 60 + 5) * 60_000).toISOString(), ...over });

  it('lets a cashier confirm the goods came back, but not record damage', async () => {
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/confirm-return` ? json(200, failed({ returnOutcome: 'returned', returnConfirmedAt: SERVER_NOW })) : undefined),
        orderHandler(() => failed()),
      ],
    });
    expect(await screen.findByRole('heading', { name: 'Did the goods come back?' })).toBeInTheDocument();
    expect(screen.getByText('Failed 2 h 5 min ago')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'No, damaged' })).not.toBeInTheDocument();
    expect(screen.getByText('Ask a manager to record damage')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Yes, all back' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/confirm-return'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/confirm-return'))?.body).toEqual({ outcome: 'returned' });
  });

  it('lets a manager record damage', async () => {
    const { calls } = renderAt('/orders/o1', {
      messages,
      handlers: [
        shops('manager'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/orders/o1/confirm-return` ? json(200, failed({ returnOutcome: 'damaged', returnConfirmedAt: SERVER_NOW })) : undefined),
        orderHandler(() => failed()),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'No, damaged' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/confirm-return'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/confirm-return'))?.body).toEqual({ outcome: 'damaged' });
    expect(screen.queryByText('Ask a manager to record damage')).not.toBeInTheDocument();
  });

  it('says an unanswered return was auto-confirmed after 72 h', async () => {
    renderAt('/orders/o1', { messages, handlers: [shops('owner'), orderHandler(() => failed({ returnOutcome: 'unconfirmed', returnConfirmedAt: SERVER_NOW }))] });
    expect(await screen.findByText(/Auto-confirmed after 72 h/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Yes, all back' })).not.toBeInTheDocument();
  });

  it('lists returns to confirm with the time since the failure, and the rider phone on the way', async () => {
    renderAt('/orders', {
      messages,
      handlers: [
        shops('cashier'),
        listHandler({
          new: [],
          returns: [summary('o3', { fulfilment: 'delivery', status: 'delivery_failed', statusReason: 'address_not_found', deliveryFailedAt: new Date(Date.now() - 35 * 60_000).toISOString(), returnOutcome: null, customerPhone: null })],
          out_for_delivery: [summary('o4', { fulfilment: 'delivery', status: 'out_for_delivery', customerPhone: '+919876543210' })],
        }),
      ],
    });
    fireEvent.click(await screen.findByRole('tab', { name: /Returns to confirm/ }));
    const row = await screen.findByRole('article', { name: 'TZ-o3' });
    expect(row).toHaveTextContent('Failed 35 min ago');
    expect(row).toHaveTextContent('Address not found');
    fireEvent.click(screen.getByRole('tab', { name: /Out for delivery/ }));
    const out = await screen.findByRole('article', { name: 'TZ-o4' });
    expect(within(out).getByRole('link', { name: 'Call +919876543210' })).toHaveAttribute('href', 'tel:+919876543210');
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
