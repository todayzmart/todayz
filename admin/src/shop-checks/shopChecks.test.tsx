import type { AdminShopCheck } from '@todayz/api-client/gen/model/adminShopCheck';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { canOpen } from '../app/nav/access';
import { NOW, STORE_ID } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';

const NEWER = '01JSH0PCHECK000000000000B1';
const OLDER = '01JSH0PCHECK000000000000B2';

function shop(over: Partial<AdminShopCheck> = {}): AdminShopCheck {
  return {
    id: NEWER,
    brandName: 'Ganesh Kirana',
    legalName: 'Ganesh Traders',
    status: 'verified',
    checkStatus: 'unchecked',
    checkNote: null,
    checkedAt: null,
    category: 'Grocery',
    liveAt: '2026-10-25T05:30:00Z',
    createdAt: '2026-10-25T05:00:00Z',
    byFieldAgent: false,
    kycStatus: 'submitted',
    stores: [
      {
        id: STORE_ID,
        name: 'Ganesh Kirana, Basavanagudi',
        status: 'live',
        addressLine1: '12 DVG Road',
        locality: 'Basavanagudi',
        city: 'Bengaluru',
        pincode: '560004',
        lat: 12.9416,
        lng: 77.5737,
        liveAt: '2026-10-25T05:30:00Z',
      },
    ],
    photos: [{ id: '01JPH0T0000000000000000P01', kind: 'storefront', storeId: STORE_ID, status: 'uploaded', previewUrl: 'https://img.todayz.localhost/p1.jpg', createdAt: NOW }],
    ...over,
  };
}

/** The queue, the check and the reject, keeping what each answer changes. */
function api(start: AdminShopCheck[] = [shop(), shop({ id: OLDER, brandName: 'Old Mart', byFieldAgent: true, photos: [], stores: [], legalName: null })]): Handler {
  let queue = start;
  return (c) => {
    if (c.method === 'GET' && c.path === '/v1/admin/shop-checks') return json(200, { items: queue, nextCursor: null, serverNow: NOW });
    const m = /^\/v1\/admin\/businesses\/([^/]+)\/(check|check-reject)$/.exec(c.path);
    if (c.method === 'POST' && m) {
      const s = queue.find((q) => q.id === m[1]);
      if (!s) return problem(409, 'invalid_transition');
      queue = queue.filter((q) => q.id !== s.id);
      if (m[2] === 'check') return json(200, { ...s, checkStatus: 'checked', checkedAt: NOW });
      return json(200, { shop: { ...s, status: 'rejected', checkStatus: 'rejected' }, cancelledOrders: 2 });
    }
    if (c.path.startsWith('/v1/admin/audit-log')) return json(200, { items: [], nextCursor: null, serverNow: NOW });
    return undefined;
  };
}

/** The nth shop card in the queue. */
function card(list: HTMLElement, n: number): HTMLElement {
  const c = within(list).getAllByRole('article')[n];
  if (!c) throw new Error(`no card ${n}`);
  return c;
}

describe('D-274 shop checks', () => {
  it('opens for field agents, admins and super admins only', () => {
    for (const r of ['field_agent', 'admin', 'super_admin']) expect(canOpen([r], 'shop-checks'), r).toBe(true);
    for (const r of ['support', 'finance', 'catalogue', 'analyst']) expect(canOpen([r], 'shop-checks'), r).toBe(false);
  });

  it('puts Shop checks in a field agent sidebar, right after Add shops', async () => {
    renderAt('/shop-checks', { roles: ['field_agent'], handlers: [api()] });
    await screen.findByRole('list', { name: 'Shops to check' });
    const links = within(screen.getByRole('navigation')).getAllByRole('link').map((a) => a.textContent);
    expect(links.indexOf('Shop checks')).toBe(links.indexOf('Add shops') + 1);
  });

  it('shows a support role no access, and calls nothing', async () => {
    const { calls } = renderAt('/shop-checks', { roles: ['support'], handlers: [api()] });
    expect(await screen.findByRole('heading', { name: "You don't have access to this screen" })).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/shop-checks')).toBe(false);
  });

  it('lists each shop with its details, stores, map pin and photos, newest first', async () => {
    const { calls } = renderAt('/shop-checks', { roles: ['field_agent'], handlers: [api()] });
    const list = await screen.findByRole('list', { name: 'Shops to check' });
    const cards = within(list).getAllByRole('article');
    expect(cards.map((c) => within(c).getByRole('heading', { level: 2 }).textContent)).toEqual(['Ganesh Kirana', 'Old Mart']);
    const first = within(card(list, 0));
    expect(first.getByText('Ganesh Traders')).toBeInTheDocument();
    expect(first.getByText(/Grocery · Live .* · Signed up by the owner/)).toBeInTheDocument();
    expect(first.getByText('Tax and bank: To review')).toBeInTheDocument();
    expect(first.getByText('12 DVG Road, Basavanagudi, Bengaluru 560004')).toBeInTheDocument();
    expect(first.getByRole('link', { name: 'Open the map pin for Ganesh Kirana, Basavanagudi' })).toHaveAttribute(
      'href',
      'https://www.google.com/maps/search/?api=1&query=12.9416,77.5737',
    );
    expect(first.getByRole('img', { name: 'Storefront photo 1' })).toHaveAttribute('src', 'https://img.todayz.localhost/p1.jpg');
    // An agent cannot read A-03 here (no businessView), so no link to it.
    expect(first.queryByRole('link', { name: 'Open business' })).toBeNull();
    const second = within(card(list, 1));
    expect(second.getByText(/Added by a field agent/)).toBeInTheDocument();
    expect(second.getByText('No store added yet.')).toBeInTheDocument();
    expect(second.getByText(/No photos yet/)).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/admin/shop-checks')?.search).toBe('?limit=20');
  });

  it('marks a shop checked with an optional note; it leaves the queue', async () => {
    const { calls } = renderAt('/shop-checks', { roles: ['field_agent'], handlers: [api()] });
    const list = await screen.findByRole('list', { name: 'Shops to check' });
    fireEvent.click(within(card(list, 0)).getByRole('button', { name: 'Mark checked' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mark Ganesh Kirana checked?' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Note' }), { target: { value: '  visited, stock matches ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mark checked' }));
       await screen.findByText('Ganesh Kirana is checked.');
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.path).toBe(`/v1/admin/businesses/${NEWER}/check`);
    expect(post?.body).toEqual({ note: 'visited, stock matches' });
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Ganesh Kirana' })).toBeNull());
    expect(screen.getByRole('heading', { name: 'Old Mart' })).toBeInTheDocument();
  });

  it('sends an empty body when the check has no note', async () => {
    const { calls } = renderAt('/shop-checks', { roles: ['admin'], handlers: [api()] });
    const list = await screen.findByRole('list', { name: 'Shops to check' });
    fireEvent.click(within(card(list, 1)).getByRole('button', { name: 'Mark checked' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mark Old Mart checked?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mark checked' }));
    await screen.findByText('Old Mart is checked.');
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({});
  });

  it('rejects only with a reason, warns what happens and says how many orders were cancelled', async () => {
    const { calls } = renderAt('/shop-checks', { roles: ['admin'], handlers: [api()] });
    const list = await screen.findByRole('list', { name: 'Shops to check' });
    const first = within(card(list, 0));
    expect(first.getByRole('link', { name: 'Open business' })).toHaveAttribute('href', `/businesses/${NEWER}`);
    fireEvent.click(first.getByRole('button', { name: 'Reject' }));
    const dialog = await screen.findByRole('dialog', { name: 'Reject Ganesh Kirana?' });
    expect(within(dialog).getByText(/hidden from customers at once/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Reason' }), { target: { value: 'Shop was closed and empty' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    await screen.findByText('Ganesh Kirana is hidden. 2 open orders cancelled.');
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.path).toBe(`/v1/admin/businesses/${NEWER}/check-reject`);
    expect(post?.body).toEqual({ reason: 'Shop was closed and empty' });
  });

  it('keeps the dialog open with the problem when the shop was already decided', async () => {
    const decided: Handler = (c) => (c.method === 'POST' ? problem(409, 'invalid_transition') : undefined);
    renderAt('/shop-checks', { roles: ['field_agent'], handlers: [decided, api()] });
    const list = await screen.findByRole('list', { name: 'Shops to check' });
    fireEvent.click(within(card(list, 0)).getByRole('button', { name: 'Mark checked' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mark Ganesh Kirana checked?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mark checked' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });

  it('says so when nothing is left to check', async () => {
    renderAt('/shop-checks', { roles: ['field_agent'], handlers: [api([])] });
    expect(await screen.findByText('Nothing to check')).toBeInTheDocument();
  });
});
