import type { AdminStore } from '@todayz/api-client/gen/model/adminStore';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, ORDER_ID, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateStoresSearch } from './search';

const BUSINESS_ID = '01JBUS1NESS0000000000000B1';

function store(over: Partial<AdminStore> = {}): AdminStore {
  return {
    id: STORE_ID,
    name: 'Lakshmi, Jayanagar',
    businessId: BUSINESS_ID,
    businessName: 'Sri Lakshmi Stores',
    businessStatus: 'verified',
    status: 'live',
    addressLine1: '12, 4th Block',
    addressLine2: null,
    landmark: null,
    locality: 'Jayanagar',
    city: 'Bengaluru',
    pincode: '560011',
    location: { lat: 12.925, lng: 77.593 },
    hours: { weekly: { mon: [{ open: '09:00', close: '21:00' }] }, holidays: [] },
    closedUntil: null,
    reviewNote: null,
    liveAt: '2026-10-21T05:00:00Z',
    reviewedAt: '2026-10-21T05:00:00Z',
    ratingAvg: 4.2,
    ratingCount: 12,
    acceptRate30d: 0.94,
    unacceptedExpiryRate30d: 0.03,
    reliabilityScore: 0.91,
    reliabilityComputedAt: '2026-10-25T00:00:00Z',
    activeStrikes: 1,
    strikes: [{ id: '01JSTRIKE00000000000000K01', reason: 'no_show', orderId: ORDER_ID, createdAt: '2026-10-23T10:00:00Z', resetAt: null, resetReason: null }],
    createdAt: '2026-10-20T05:00:00Z',
    serverNow: NOW,
    ...over,
  };
}

function api(start = store(), opts: { hoursError?: boolean } = {}): Handler {
  let current = start;
  const base = `/v1/admin/stores/${STORE_ID}`;
  return (c) => {
    if (c.path === '/v1/admin/stores' && c.method === 'GET') {
      const { id, name, businessId, businessName, businessStatus, status, locality, city, closedUntil, activeStrikes, reliabilityScore, createdAt } = current;
      const row = { id, name, businessId, businessName, businessStatus, status, locality, city, closedUntil, activeStrikes, reliabilityScore, createdAt };
      return json(200, { items: [row], nextCursor: null, serverNow: NOW });
    }
    if (c.path === base && c.method === 'GET') return json(200, current);
    if (c.path === base && c.method === 'PATCH') {
      if (opts.hoursError)
        return json(422, { type: 'about:blank#validation_failed', title: 'x', status: 422, code: 'validation_failed', fields: { 'body.hours.weekly.mon[1]': 'overlap' } });
      const b = c.body as Pick<AdminStore, 'hours'>;
      current = { ...current, hours: b.hours };
      return json(200, current);
    }
    if (c.path === `${base}/force-close`) {
      current = { ...current, closedUntil: (c.body as { until: string }).until };
      return json(200, current);
    }
    if (c.path === `${base}/review`) {
      if (current.businessStatus !== 'verified') return problem(409, 'invalid_transition');
      current = { ...current, status: 'live', liveAt: NOW };
      return json(200, current);
    }
    if (c.path === `${base}/strikes/reset`) {
      const reason = (c.body as { reason: string }).reason;
      current = { ...current, activeStrikes: 0, strikes: current.strikes.map((k) => ({ ...k, resetAt: NOW, resetReason: reason })) };
      return json(200, current);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('stores search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateStoresSearch({ status: 'paused', city: 'Bengaluru', x: 1 })).toEqual({ status: 'paused', city: 'Bengaluru' });
    expect(validateStoresSearch({ tab: 'review' })).toEqual({ tab: 'review' });
    expect(validateStoresSearch({ tab: 'other', status: 'closed', city: '<b>' })).toEqual({});
  });
});

describe('A-05 stores', () => {
  it('filters by status and city in the URL, and lists the review queue', async () => {
    const { calls, router } = renderAt('/stores', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Stores' });
    expect(await within(table).findByRole('link', { name: 'Lakshmi, Jayanagar' })).toHaveAttribute('href', `/stores/${STORE_ID}`);
    const form = screen.getByRole('search', { name: 'Store filters' });
    fireEvent.change(within(form).getByLabelText('Status'), { target: { value: 'live' } });
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'live' }));
    fireEvent.change(within(form).getByLabelText('City'), { target: { value: ' Bengaluru ' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'live', city: 'Bengaluru' }));
    const last = () => new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/stores').at(-1)?.search);
    await waitFor(() => expect(last().get('city')).toBe('Bengaluru'));
    expect(last().get('status')).toBe('live');

    fireEvent.click(screen.getByRole('tab', { name: 'To review' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'review' }));
    await waitFor(() => expect(last().get('status')).toBe('pending_review'));
    expect(last().get('city')).toBeNull();
  });

  it('edits hours and shows a server field error on its day', async () => {
    const { calls } = renderAt(`/stores/${STORE_ID}`, { roles: ['admin'], handlers: [api(store(), { hoursError: true })] });
    fireEvent.click(await screen.findByRole('button', { name: 'Edit pin and hours' }));
    const dialog = await screen.findByRole('dialog', { name: 'Edit Lakshmi, Jayanagar' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add a range on Monday' }));
    fireEvent.change(within(dialog).getByLabelText('Monday range 2 opens'), { target: { value: '20:00' } });
    fireEvent.change(within(dialog).getByLabelText('Monday range 2 closes'), { target: { value: '22:00' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Owner asked by phone' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }));
    expect(await within(dialog).findByText('Check the Monday ranges: they overlap or a time is wrong.')).toBeInTheDocument();
    expect(within(dialog).queryByText(/Tuesday ranges/)).not.toBeInTheDocument();
    const body = calls.find((c) => c.method === 'PATCH')?.body as { reason: string; hours: { weekly: Record<string, unknown> }; location: unknown };
    expect(body.reason).toBe('Owner asked by phone');
    expect(body.hours.weekly.mon).toEqual([
      { open: '09:00', close: '21:00' },
      { open: '20:00', close: '22:00' },
    ]);
    expect(body.location).toEqual({ lat: 12.925, lng: 77.593 });
  });

  it('blocks a force-close until a future IST time is picked', async () => {
    const { calls } = renderAt(`/stores/${STORE_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Close for a while' }));
    const dialog = await screen.findByRole('dialog', { name: 'Close Lakshmi, Jayanagar for a while?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Owner unwell' } });
    // NOW is 12:00 IST on 25 Oct.
    fireEvent.change(within(dialog).getByLabelText('Closed until (IST)'), { target: { value: '2026-10-25T11:00' } });
    expect(within(dialog).getByText('This time has passed. Pick a time in the future.')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close for a while' }));
    expect(calls.some((c) => c.path.endsWith('/force-close'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Closed until (IST)'), { target: { value: '2026-10-26T09:00' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close for a while' }));
    await waitFor(() => expect(screen.getByText('Store closed')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/force-close'))?.body).toEqual({ reason: 'Owner unwell', until: '2026-10-26T03:30:00.000Z' });
  });

  it('keeps approve disabled until the business is verified', async () => {
    renderAt(`/stores/${STORE_ID}`, { roles: ['admin'], handlers: [api(store({ status: 'pending_review', businessStatus: 'submitted', liveAt: null }))] });
    expect(await screen.findByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(screen.getByText('The business must be verified before this store can go live.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Send back' })).toBeEnabled();
  });

  it('approves a store of a verified business', async () => {
    const { calls } = renderAt(`/stores/${STORE_ID}`, { roles: ['admin'], handlers: [api(store({ status: 'pending_review', liveAt: null }))] });
    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }));
    const dialog = await screen.findByRole('dialog', { name: 'Approve Lakshmi, Jayanagar?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(screen.getByText('Store approved')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/review'))?.body).toEqual({ decision: 'approve' });
  });

  it('lets support reset strikes but not edit, close or review', async () => {
    const { calls } = renderAt(`/stores/${STORE_ID}`, { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reset strikes' }));
    expect(screen.queryByRole('button', { name: 'Edit pin and hours' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Close for a while' })).not.toBeInTheDocument();
    const dialog = await screen.findByRole('dialog', { name: 'Reset 1 strike?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Customer confirmed pickup' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reset strikes' }));
    await waitFor(() => expect(screen.getByText('Strikes reset')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/strikes/reset'))?.body).toEqual({ reason: 'Customer confirmed pickup' });
    expect(screen.getByRole('button', { name: 'Reset strikes' })).toBeDisabled();
  });
});
