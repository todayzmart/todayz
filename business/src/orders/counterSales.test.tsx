import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';
import { addDays, istToday, parseSalesSearch, rangeDays } from './salesQueries';

// Orders → Counter sales (D-268): the store's POS sales next to online orders.
const BASE = '/v1/biz/businesses/b1/stores/s1';
const SERVER_NOW = new Date().toISOString();
const SALE = '01J9ZQQQQQQQQQQQQQQQQQQQ01';
const FAILED = '01J9ZQQQQQQQQQQQQQQQQQQQ02';
const GONE = '01J9ZQQQQQQQQQQQQQQQQQQQ03';
const RAVI = { id: '01J9ZUUUUUUUUUUUUUUUUUUU01', name: 'Ravi' };

function shops(role: string): Handler {
  return (c) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function row(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    clientId: id,
    soldAt: '2026-10-27T05:30:00.000Z',
    receivedAt: '2026-10-27T05:30:02.000Z',
    totalPaise: 38200,
    lineCount: 3,
    unitCount: 4,
    paymentMethod: 'upi',
    creditsUsed: 20,
    cashbackCredits: 0,
    status: 'completed',
    reversedAt: null,
    offline: false,
    link: { method: 'bill_qr', status: 'linked', customerName: 'Ananya', failure: null },
    cashier: RAVI,
    ...over,
  };
}

const ROWS = [
  row(SALE),
  row(FAILED, { totalPaise: 9600, paymentMethod: 'cash', offline: true, link: { method: 'counter_code', status: 'failed', customerName: null, failure: 'code_used' } }),
  row(GONE, { totalPaise: 1000, status: 'reversed', reversedAt: '2026-10-27T06:00:00.000Z', link: null }),
];

function detail(id: string, over: Record<string, unknown> = {}) {
  const base = ROWS.find((r) => r.id === id) ?? row(id);
  return {
    ...base,
    lines: [
      { id: `${id.slice(0, 24)}L1`, clientId: `${id.slice(0, 24)}L1`, storeItemId: 'i1', name: 'Basmati rice 1 kg', unitPricePaise: 12000, quantity: 2, offerDiscountPaise: 1000, manualDiscountPaise: 0, lineTotalPaise: 23000 },
      { id: `${id.slice(0, 24)}L2`, clientId: `${id.slice(0, 24)}L2`, storeItemId: 'i2', name: 'Tea 250 g', unitPricePaise: 15200, quantity: 1, offerDiscountPaise: 0, manualDiscountPaise: 0, lineTotalPaise: 15200 },
    ],
    itemSubtotalPaise: 38200,
    billOfferDiscountPaise: 0,
    subtotalPaise: 38200,
    discountPaise: 0,
    roundOffPaise: 0,
    deviceId: '01J9ZDDDDDDDDDDDDDDDDDD7K2',
    reversalReason: base.status === 'reversed' ? 'Wrong item billed' : null,
    reversedBy: base.status === 'reversed' ? { id: '01J9ZUUUUUUUUUUUUUUUUUUU09', name: 'Asha' } : null,
    reversibleUntil: base.status === 'completed' ? new Date(Date.now() + 3_600_000).toISOString() : null,
    version: 1,
    serverNow: SERVER_NOW,
    ...over,
  };
}

const SUMMARY = {
  from: null,
  to: null,
  salesPaise: 47800,
  billCount: 2,
  averageBillPaise: 23900,
  unitCount: 8,
  reversedPaise: 1000,
  reversedCount: 1,
  creditsUsed: 40,
  cashbackCredits: 3,
  offlineCount: 1,
  linkedCount: 1,
  byPayment: [
    { method: 'upi', billCount: 1, totalPaise: 38200 },
    { method: 'cash', billCount: 1, totalPaise: 9600 },
  ],
  byCashier: [{ cashier: RAVI, billCount: 2, totalPaise: 47800 }],
  serverNow: SERVER_NOW,
};

function salesApi(over: { detail?: Record<string, unknown> } = {}): Handler {
  return (c) => {
    if (c.method === 'GET' && c.path === `${BASE}/pos/sales`) return json(200, { items: ROWS, nextCursor: null, serverNow: SERVER_NOW });
    if (c.method === 'GET' && c.path === `${BASE}/pos/sales-summary`) return json(200, SUMMARY);
    if (c.method === 'GET' && c.path === `${BASE}/pos/sales-export`) return new Response('bill,total\n', { status: 200, headers: { 'content-type': 'text/csv' } });
    if (c.method === 'GET' && c.path.startsWith(`${BASE}/pos/sales/`)) return json(200, detail(c.path.split('/').pop() ?? '', over.detail));
    if (c.method === 'POST' && c.path === `${BASE}/pos/sales/${SALE}/reverse`) return json(200, { ...detail(SALE), status: 'reversed' });
    return undefined;
  };
}

const lists = (calls: Call[]) => calls.filter((c) => c.method === 'GET' && c.path === `${BASE}/pos/sales`);
const params = (c: Call | undefined) => Object.fromEntries(new URLSearchParams(c?.search ?? ''));

beforeEach(() => {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: false, media: query, addEventListener: () => undefined, removeEventListener: () => undefined }));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sales search', () => {
  it('keeps only known values and maps the ranges to IST days', () => {
    expect(parseSalesSearch({ view: 'sales', range: '7d', payment: 'cheque', link: 'failed', offline: 'true', cashier: 'x', sale: SALE })).toEqual({
      view: 'sales',
      range: '7d',
      link: 'failed',
      offline: true,
      sale: SALE,
    });
    expect(parseSalesSearch({ payment: 'upi' })).toEqual({});
    expect(rangeDays({ range: '7d' }, '2026-10-27')).toEqual({ from: '2026-10-21', to: '2026-10-27' });
    expect(rangeDays({ range: 'yesterday' }, '2026-11-01')).toEqual({ from: '2026-10-31', to: '2026-10-31' });
    expect(rangeDays({ range: 'custom', from: '2026-10-01' }, '2026-10-27')).toEqual({ from: '2026-10-01' });
    // 19:00 UTC is already the next day in IST.
    expect(istToday(Date.parse('2026-10-27T19:00:00Z'))).toBe('2026-10-28');
  });
});

describe('Counter sales', () => {
  it('switches from Online orders to Counter sales with its own URL', async () => {
    const view = renderAt('/orders', { handlers: [shops('manager'), salesApi()] });
    fireEvent.click(await screen.findByRole('radio', { name: 'Counter sales' }));
    const table = await screen.findByRole('table', { name: 'Counter sales' });
    expect(view.history.location.search).toContain('view=sales');
    expect(within(table).getByText('Linked to Ananya')).toBeInTheDocument();
    expect(within(table).getByText('The counter code was already used.')).toBeInTheDocument();
    expect(within(table).getAllByText('Ravi')).toHaveLength(3);
    fireEvent.click(screen.getByRole('radio', { name: 'Online orders' }));
    await waitFor(() => expect(view.history.location.search).not.toContain('view=sales'));
  });

  it('sends the dates and filters from the URL and puts a new filter back in it', async () => {
    const view = renderAt('/orders?view=sales&range=7d&payment=upi&link=pending&offline=true', { handlers: [shops('owner'), salesApi()] });
    await screen.findByRole('table', { name: 'Counter sales' });
    const today = istToday(Date.now());
    expect(params(lists(view.calls)[0])).toMatchObject({ from: addDays(today, -6), to: today, payment: 'upi', link: 'pending', offline: 'true' });
    const summary = view.calls.find((c) => c.path === `${BASE}/pos/sales-summary`);
    expect(params(summary)).toMatchObject({ from: addDays(today, -6), to: today, payment: 'upi' });

    fireEvent.change(screen.getByRole('combobox', { name: 'Status' }), { target: { value: 'reversed' } });
    await waitFor(() => expect(params(lists(view.calls).at(-1))).toMatchObject({ status: 'reversed', payment: 'upi' }));
    expect(view.history.location.search).toContain('status=reversed');

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() => expect(params(lists(view.calls).at(-1))).not.toHaveProperty('payment'));
    expect(params(lists(view.calls).at(-1))).toMatchObject({ from: addDays(today, -6), to: today });
  });

  it('shows the totals, the payment split and the cashier filter to a manager', async () => {
    renderAt('/orders?view=sales', { handlers: [shops('manager'), salesApi()] });
    const strip = await screen.findByRole('region', { name: 'Counter sales at a glance' });
    await within(strip).findByText('₹478');
    expect(within(strip).getByText('₹239')).toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'By payment' })).toHaveTextContent('UPI');
    expect(within(screen.getByRole('combobox', { name: 'Cashier' })).getByRole('option', { name: 'Ravi' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeInTheDocument();
  });

  it('gives a cashier the list only: no totals call, no cashier filter, no export', async () => {
    const view = renderAt('/orders?view=sales', { handlers: [shops('cashier'), salesApi()] });
    await screen.findByRole('table', { name: 'Counter sales' });
    expect(view.calls.some((c) => c.path === `${BASE}/pos/sales-summary`)).toBe(false);
    expect(screen.queryByRole('region', { name: 'Counter sales at a glance' })).toBeNull();
    expect(screen.queryByRole('combobox', { name: 'Cashier' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Export CSV' })).toBeNull();
  });

  it('opens a bill in the panel with its lines, people, times and link failure', async () => {
    const view = renderAt('/orders?view=sales', { handlers: [shops('cashier'), salesApi()] });
    const table = await screen.findByRole('table', { name: 'Counter sales' });
    fireEvent.click(within(table).getByRole('button', { name: 'Open bill #QQQQ02' }));
    const panel = await screen.findByRole('dialog', { name: 'Bill #QQQQ02' });
    expect(view.history.location.search).toContain(`sale=${FAILED}`);
    await within(panel).findByText('Basmati rice 1 kg');
    expect(within(panel).getByText(/2 × ₹120/)).toBeInTheDocument();
    expect(within(panel).getByText(/₹10 off/)).toBeInTheDocument();
    expect(within(panel).getByText('Ravi')).toBeInTheDocument();
    expect(within(panel).getByText('#DDD7K2')).toBeInTheDocument();
    expect(within(panel).getByText('The counter code was already used.')).toBeInTheDocument();
    expect(within(panel).getByText(FAILED)).toBeInTheDocument();
    expect(within(panel).getByText('The receipt opens on the device that rang up this sale.')).toBeInTheDocument();
    // A cashier cannot reverse (pos.reverse is manager and above).
    expect(within(panel).queryByRole('button', { name: 'Reverse' })).toBeNull();
  });

  it('shows who reversed a bill, when and why', async () => {
    renderAt(`/orders?view=sales&sale=${GONE}`, { handlers: [shops('owner'), salesApi()] });
    const panel = await screen.findByRole('dialog', { name: 'Bill #QQQQ03' });
    const reversal = await within(panel).findByRole('region', { name: 'Reversal' });
    expect(reversal).toHaveTextContent('Wrong item billed');
    expect(reversal).toHaveTextContent('Asha');
    expect(within(panel).queryByRole('button', { name: 'Reverse' })).toBeNull();
  });

  it('lets a manager reverse a bill with a reason and the per-sale idempotency key', async () => {
    const view = renderAt(`/orders?view=sales&sale=${SALE}`, { handlers: [shops('manager'), salesApi()] });
    const panel = await screen.findByRole('dialog', { name: 'Bill #QQQQ01' });
    fireEvent.click(await within(panel).findByRole('button', { name: 'Reverse' }));
    const confirm = await screen.findByRole('alertdialog');
    const go = within(confirm).getByRole('button', { name: 'Reverse sale' });
    expect(go).toBeDisabled();
    fireEvent.change(within(confirm).getByLabelText('Reason'), { target: { value: 'Customer returned it' } });
    fireEvent.click(go);
    await screen.findByText('Sale reversed');
    const post = view.calls.find((c) => c.method === 'POST' && c.path.endsWith('/reverse'));
    expect(post?.body).toEqual({ reason: 'Customer returned it' });
    expect(post?.headers.get('Idempotency-Key')).toBe(`reverse-${SALE}`);
  });

  it('hides Reverse once the window has passed', async () => {
    renderAt(`/orders?view=sales&sale=${SALE}`, { handlers: [shops('owner'), salesApi({ detail: { reversibleUntil: new Date(Date.now() - 60_000).toISOString() } })] });
    const panel = await screen.findByRole('dialog', { name: 'Bill #QQQQ01' });
    await within(panel).findByText('Basmati rice 1 kg');
    expect(within(panel).queryByRole('button', { name: 'Reverse' })).toBeNull();
  });

  it('downloads the filtered CSV for a manager', async () => {
    const created = vi.fn(() => 'blob:csv');
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: created, revokeObjectURL: () => undefined }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    const view = renderAt('/orders?view=sales&range=yesterday&payment=cash', { handlers: [shops('manager'), salesApi()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Export CSV' }));
    await waitFor(() => expect(click).toHaveBeenCalled());
    const yesterday = addDays(istToday(Date.now()), -1);
    const call = view.calls.find((c) => c.path === `${BASE}/pos/sales-export`);
    expect(params(call)).toMatchObject({ from: yesterday, to: yesterday, payment: 'cash' });
    expect(created).toHaveBeenCalled();
  });
});
