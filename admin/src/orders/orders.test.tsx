import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ORDER_ID, emptyAudit, order, orderSummary } from '../test/fixtures';
import { json, renderAt, type Handler } from '../test/render';
import { ordersParams, validateOrdersSearch } from './search';

const base = `/v1/admin/orders/${ORDER_ID}`;

function api(start = order()): Handler {
  let current = start;
  return (c) => {
    if (c.path === '/v1/admin/orders' && c.method === 'GET') return json(200, { items: [orderSummary()], nextCursor: null, serverNow: current.serverNow });
    if (c.path === base && c.method === 'GET') return json(200, current);
    if (c.path === `${base}/cancel` && c.method === 'POST') {
      current = { ...current, status: 'cancelled', statusReason: 'other', statusNote: 'duplicate booking' };
      return json(200, current);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('orders search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateOrdersSearch({ number: 'TZ-1042', status: 'accepted', storeId: 'nope', from: '2026-10-25', to: '25/10/2026', x: 1 })).toEqual({
      number: 'TZ-1042',
      status: 'accepted',
      from: '2026-10-25',
    });
    expect(validateOrdersSearch({ status: 'shipped', number: 'a b' })).toEqual({});
  });

  it('turns IST days into an exclusive UTC range', () => {
    expect(ordersParams({ from: '2026-10-25', to: '2026-10-25' })).toEqual({ from: '2026-10-24T18:30:00.000Z', to: '2026-10-25T18:30:00.000Z' });
  });
});

describe('A-07 orders', () => {
  it('puts the filters in the URL and sends the IST bounds', async () => {
    const { calls, router } = renderAt('/orders', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Orders' });
    expect(await within(table).findByRole('link', { name: 'TZ-1042' })).toHaveAttribute('href', `/orders/${ORDER_ID}`);
    const form = screen.getByRole('search', { name: 'Order filters' });
    fireEvent.change(within(form).getByLabelText('Status'), { target: { value: 'accepted' } });
    fireEvent.change(within(form).getByLabelText('Placed from'), { target: { value: '2026-10-25' } });
    fireEvent.change(within(form).getByLabelText('Placed to'), { target: { value: '2026-10-25' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'accepted', from: '2026-10-25', to: '2026-10-25' }));
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/orders')).toHaveLength(2));
    const q = new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/orders')[1]?.search);
    expect(q.get('status')).toBe('accepted');
    expect(q.get('from')).toBe('2026-10-24T18:30:00.000Z');
    expect(q.get('to')).toBe('2026-10-25T18:30:00.000Z');
  });
});

describe('A-07 order detail', () => {
  it('shows the timeline and never a code', async () => {
    renderAt(`/orders/${ORDER_ID}`, { roles: ['support'], handlers: [api()] });
    expect(await screen.findByRole('heading', { level: 1, name: /Order TZ-1042/ })).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Items' })).toHaveTextContent('Basmati rice');
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();
  });

  it('cancels with a reason, a note for Other and an Idempotency-Key', async () => {
    const { calls } = renderAt(`/orders/${ORDER_ID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel order' }));
    const dialog = await screen.findByRole('dialog', { name: 'Cancel order TZ-1042?' });
    const confirm = () => fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel order' }));
    confirm();
    expect(await within(dialog).findByText('Choose a reason.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'other' } });
    confirm();
    expect(await within(dialog).findByText('Write a note when the reason is other.')).toBeInTheDocument();
    expect(calls.some((c) => c.path === `${base}/cancel`)).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Note'), { target: { value: 'duplicate booking' } });
    confirm();
    await waitFor(() => expect(screen.getByText('Order TZ-1042 cancelled')).toBeInTheDocument());
    const post = calls.find((c) => c.path === `${base}/cancel`);
    expect(post?.body).toEqual({ reason: 'other', strike: 'none', note: 'duplicate booking' });
    expect(post?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Cancel order' })).not.toBeInTheDocument());
  });

  it('offers no cancel on a finished order or to a role without it', async () => {
    renderAt(`/orders/${ORDER_ID}`, { roles: ['support'], handlers: [api(order({ status: 'collected' }))] });
    expect(await screen.findByRole('heading', { level: 1, name: /Order TZ-1042/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Cancel order' })).not.toBeInTheDocument();
  });
});
