import type { AdminBanner } from '@todayz/api-client/gen/model/adminBanner';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { canTakeDown, waiting } from './labels';
import { validateBannersSearch } from './search';

const BANNER_ID = '01JBANNER00000000000000B01';

function banner(over: Partial<AdminBanner> = {}): AdminBanner {
  return {
    id: BANNER_ID,
    storeId: STORE_ID,
    storeName: 'Sri Lakshmi Stores',
    storeCity: 'Bengaluru',
    headline: 'Diwali sweets 20% off',
    imageKey: null,
    template: { tint: 'amber', subline: 'Kaju katli, laddoo and more' },
    targetType: 'offer',
    targetLabel: 'Diwali sweets',
    targetItemCount: 0,
    startsAt: '2026-10-24T00:00:00Z',
    endsAt: '2026-11-02T00:00:00Z',
    status: 'pending_review',
    moderation: 'needs_review',
    removeReason: null,
    removedAt: null,
    createdAt: '2026-10-24T10:00:00Z',
    updatedAt: '2026-10-24T10:00:00Z',
    ...over,
  };
}

function api(start = banner()): Handler {
  let current = start;
  const state = (b: AdminBanner) => (b.status === 'pending_review' ? 'review' : b.status === 'removed' ? 'removed' : 'live');
  return (c) => {
    if (c.path === '/v1/admin/banners' && c.method === 'GET') {
      const want = new URLSearchParams(c.search).get('state') ?? 'review';
      return json(200, { items: state(current) === want ? [current] : [], nextCursor: null, serverNow: NOW });
    }
    if (c.path === `/v1/admin/banners/${BANNER_ID}/approve` && c.method === 'POST') {
      if (!waiting(current)) return problem(409, 'invalid_transition');
      current = { ...current, status: 'live', moderation: 'auto_ok' };
      return json(200, current);
    }
    if (c.path === `/v1/admin/banners/${BANNER_ID}/takedown` && c.method === 'POST') {
      if (!canTakeDown(current)) return problem(409, 'invalid_transition');
      current = { ...current, status: 'removed', removeReason: (c.body as { reason: string }).reason, removedAt: NOW };
      return json(200, current);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('banners search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateBannersSearch({ state: 'removed', storeId: STORE_ID, banner: BANNER_ID, y: 1 })).toEqual({
      state: 'removed',
      storeId: STORE_ID,
      banner: BANNER_ID,
    });
    expect(validateBannersSearch({ state: 'review', storeId: 'x', banner: 'nope' })).toEqual({});
  });
});

describe('A-20 banners', () => {
  it('lists the review queue first and puts the store filter in the URL', async () => {
    const { calls, router } = renderAt('/banners', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Waiting for review' });
    expect(await within(table).findByText('Sri Lakshmi Stores, Bengaluru')).toBeInTheDocument();
    expect(within(table).getByText('Offer: Diwali sweets')).toBeInTheDocument();
    expect(within(table).getByText('Template')).toBeInTheDocument();
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/banners')?.search).get('state')).toBe('review');

    const form = screen.getByRole('search', { name: 'Banner filters' });
    fireEvent.change(within(form).getByLabelText('Store ID'), { target: { value: STORE_ID.toLowerCase() } });
    fireEvent.click(within(form).getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ storeId: STORE_ID }));
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/banners')).toHaveLength(2));
    expect(new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/banners')[1]?.search).get('storeId')).toBe(STORE_ID);

    fireEvent.click(screen.getByRole('tab', { name: 'Taken down' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ state: 'removed', storeId: STORE_ID }));
    expect(await screen.findByText('No banners here.')).toBeInTheDocument();
  });

  it('clears a held banner with an optional note', async () => {
    const { calls, router } = renderAt('/banners', { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Diwali sweets 20% off' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ banner: BANNER_ID }));
    expect(await screen.findByRole('heading', { name: 'Banner from Sri Lakshmi Stores' })).toBeInTheDocument();
    expect(screen.getByText('Kaju katli, laddoo and more')).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Clear banner' }));
    const dialog = await screen.findByRole('dialog', { name: 'Clear the banner from Sri Lakshmi Stores?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear banner' }));
    await waitFor(() => expect(screen.getByText('Banner cleared')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/banners/${BANNER_ID}/approve`)?.body).toEqual({});
    expect(await screen.findByText('That banner is not in this list. It may have moved to another tab.')).toBeInTheDocument();
  });

  it('sends the note when one is written', async () => {
    const { calls } = renderAt(`/banners?banner=${BANNER_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Clear banner' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Note'), { target: { value: '  photo is the shop front  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear banner' }));
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/approve'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/approve'))?.body).toEqual({ reason: 'photo is the shop front' });
  });

  it('takes a live banner down only with a reason', async () => {
    const live = banner({ status: 'live', moderation: 'auto_ok' });
    const { calls } = renderAt(`/banners?state=live&banner=${BANNER_ID}`, { roles: ['super_admin'], handlers: [api(live)] });
    expect(await screen.findByRole('heading', { name: 'Banner from Sri Lakshmi Stores' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clear banner' })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Take down' }));
    const dialog = await screen.findByRole('dialog', { name: 'Take down the banner from Sri Lakshmi Stores?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Take down' }));
    expect(await within(dialog).findByText(/Write a reason/)).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/takedown'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'misleading price' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Take down' }));
    await waitFor(() => expect(screen.getByText('Banner taken down')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/banners/${BANNER_ID}/takedown`)?.body).toEqual({ reason: 'misleading price' });
  });

  it('shows the reason on a taken-down banner and offers no moves', async () => {
    const gone = banner({ status: 'removed', moderation: 'auto_ok', removeReason: 'misleading price', removedAt: NOW });
    renderAt(`/banners?state=removed&banner=${BANNER_ID}`, { roles: ['support'], handlers: [api(gone)] });
    expect(await screen.findByText('Reason: misleading price')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Take down' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clear banner' })).not.toBeInTheDocument();
  });

  it('shows the 409 when someone else decided first', async () => {
    const handler: Handler = (c) => (c.path.endsWith('/takedown') ? problem(409, 'invalid_transition') : api(banner({ status: 'live', moderation: 'auto_ok' }))(c));
    renderAt(`/banners?state=live&banner=${BANNER_ID}`, { roles: ['support'], handlers: [handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Take down' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'spam' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Take down' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });
});
