import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BIZ = '/v1/biz/businesses';

function shops(role: string, invitations: unknown[] = [], status = 'verified') {
  return (c: Call) =>
    c.method === 'GET' && c.path === BIZ
      ? json(200, {
          items: [{ id: 'b1', status, brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }],
          invitations,
          nextCursor: null,
        })
      : undefined;
}

function store(over: Record<string, unknown> = {}) {
  return {
    id: 's1',
    businessId: 'b1',
    slug: 'indiranagar',
    shortCode: 'IND1',
    name: 'Indiranagar',
    addressLine1: '12 Main Road',
    addressLine2: null,
    landmark: null,
    locality: 'Indiranagar',
    city: 'Bengaluru',
    pincode: '560038',
    location: { lat: 12.97, lng: 77.64 },
    phone: '+919876543210',
    hours: { weekly: {}, holidays: [] },
    status: 'live',
    openNow: true,
    createdAt: '2026-10-01T00:00:00.000Z',
    ...over,
  };
}

function member(id: string, role: string, over: Record<string, unknown> = {}) {
  return {
    id,
    userId: `u-${id}`,
    name: id,
    phoneMasked: '+91 98xxx xx210',
    inviteExpiresAt: null,
    role,
    storeIds: null,
    canDeliver: false,
    status: 'active',
    createdAt: '2026-10-01T00:00:00.000Z',
    ...over,
  };
}

const TEAM = [member('Asha', 'owner'), member('Manoj', 'manager'), member('Chitra', 'cashier')];

function lists(stores: unknown[] = [store()], staff: unknown[] = TEAM): Handler {
  return (c) => {
    if (c.method !== 'GET') return undefined;
    if (c.path === `${BIZ}/b1/stores`) return json(200, { items: stores, nextCursor: null });
    if (c.path === `${BIZ}/b1/staff`) return json(200, { items: staff, nextCursor: null });
    return undefined;
  };
}

beforeEach(() => {
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
});
afterEach(() => vi.unstubAllGlobals());

describe('B-13 stores and staff by role', () => {
  it('owner sees stores and staff; Edit on non-owners, Remove on non-owners', async () => {
    renderAt('/stores', { messages, handlers: [shops('owner'), lists()] });
    expect(await screen.findByRole('heading', { name: 'Stores' })).toBeInTheDocument();
    expect(await screen.findByText('Manoj')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add a store' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit Manoj' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit Chitra' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Edit Asha' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove Asha' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Remove Manoj' })).toBeInTheDocument();
  });

  it('owner invites a manager or a cashier and the POST carries an Idempotency-Key', async () => {
    const { calls } = renderAt('/stores', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/staff` ? json(201, member('new', 'manager', { status: 'invited' })) : undefined),
        lists(),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Invite someone' }));
    const sheet = await screen.findByRole('dialog', { name: 'Invite someone' });
    expect(within(sheet).getByRole('radio', { name: /Manager/ })).toBeInTheDocument();
    expect(within(sheet).getByRole('radio', { name: /Cashier/ })).toBeInTheDocument();
    fireEvent.change(within(sheet).getByLabelText('Mobile number'), { target: { value: '9876543210' } });
    fireEvent.click(within(sheet).getByRole('radio', { name: /Manager/ }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Send invite' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === `${BIZ}/b1/staff`)).toBe(true));
    const post = calls.find((c) => c.method === 'POST' && c.path === `${BIZ}/b1/staff`);
    expect(post?.body).toEqual({ phone: '+919876543210', role: 'manager', storeIds: null, canDeliver: false });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByText('Invite sent')).toBeInTheDocument();
  });

  it('invite refuses a number that is not a 10-digit mobile', async () => {
    const { calls } = renderAt('/stores', { messages, handlers: [shops('owner'), lists()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Invite someone' }));
    const sheet = await screen.findByRole('dialog', { name: 'Invite someone' });
    fireEvent.change(within(sheet).getByLabelText('Mobile number'), { target: { value: '12345' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Send invite' }));
    expect(await within(sheet).findByText('Enter a 10-digit mobile number.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('manager can invite only a cashier, cannot edit staff, and removes cashiers only', async () => {
    renderAt('/stores', { messages, handlers: [shops('manager'), lists()] });
    expect(await screen.findByText('Chitra')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Edit (Manoj|Chitra|Asha)$/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Remove Chitra' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove Manoj' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove Asha' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Invite someone' }));
    const sheet = await screen.findByRole('dialog', { name: 'Invite someone' });
    expect(within(sheet).getByRole('radio', { name: /Cashier/ })).toBeChecked();
    expect(within(sheet).queryByRole('radio', { name: /Manager/ })).not.toBeInTheDocument();
  });

  it('cashier gets the no-access state and the staff list is never requested', async () => {
    const { calls } = renderAt('/stores', { messages, handlers: [shops('cashier'), lists()] });
    expect(await screen.findByText('Owners and managers only')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add a store' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Invite someone' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/staff') || c.path.endsWith('/stores'))).toBe(false);
  });

  it('remove asks first, then calls DELETE', async () => {
    const { calls } = renderAt('/stores', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'DELETE' && c.path === `${BIZ}/b1/staff/Chitra` ? new Response(null, { status: 204 }) : undefined), lists()],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Remove Chitra' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.path === `${BIZ}/b1/staff/Chitra`)).toBe(true));
  });

  it('editing a store reads the ETag, sends If-Match and only the changed field', async () => {
    const { calls } = renderAt('/stores', {
      messages,
      handlers: [
        shops('manager'),
        (c) => (c.method === 'GET' && c.path === `${BIZ}/b1/stores/s1` ? json(200, store(), { etag: '"v3"' }) : undefined),
        (c) => (c.method === 'PATCH' && c.path === `${BIZ}/b1/stores/s1` ? json(200, store({ name: 'Indiranagar 2' }), { etag: '"v4"' }) : undefined),
        lists(),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Edit Indiranagar' }));
    const sheet = await screen.findByRole('dialog', { name: 'Edit store' });
    fireEvent.change(within(sheet).getByLabelText('Store name'), { target: { value: 'Indiranagar 2' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Save store' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch?.headers.get('if-match')).toBe('"v3"');
    expect(patch?.body).toEqual({ name: 'Indiranagar 2' });
  });

  it('a 412 shows the precondition message', async () => {
    renderAt('/stores', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.method === 'GET' && c.path === `${BIZ}/b1/stores/s1` ? json(200, store(), { etag: '"v3"' }) : undefined),
        (c) => (c.method === 'PATCH' ? problem(412, 'precondition_failed') : undefined),
        lists(),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Edit Indiranagar' }));
    const sheet = await screen.findByRole('dialog', { name: 'Edit store' });
    fireEvent.change(within(sheet).getByLabelText('City'), { target: { value: 'Mysuru' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Save store' }));
    expect(await within(sheet).findByText(/Someone changed this while you were editing/)).toBeInTheDocument();
  });

  it('a draft store can be sent for review (Idempotency-Key); a live one cannot', async () => {
    const { calls } = renderAt('/stores', {
      messages,
      handlers: [
        shops('owner', [], 'draft'),
        (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/stores/s2/submit` ? json(200, store({ id: 's2', name: 'Whitefield', status: 'pending_review' })) : undefined),
        lists([store(), store({ id: 's2', name: 'Whitefield', status: 'draft' })]),
      ],
    });
    expect(await screen.findByRole('button', { name: 'Send Whitefield for review' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send Indiranagar for review' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Send Whitefield for review' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path.endsWith('/s2/submit'))).toBe(true));
    expect(calls.find((c) => c.path.endsWith('/s2/submit'))?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('only the owner is pointed back to onboarding while the business is a draft', async () => {
    renderAt('/stores', { messages, handlers: [shops('manager', [], 'draft'), lists()] });
    expect(await screen.findByText('Setup not sent yet')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Continue setup' })).not.toBeInTheDocument();
  });
});

describe('invite page', () => {
  const invitation = { staffId: 'st1', businessId: 'b9', brandName: 'Green Basket', role: 'cashier', invitedAt: '2026-10-23T00:00:00.000Z' };

  it('lists the invitation for the link and accepting posts to the accept route', async () => {
    const { calls, history } = renderAt('/invite/st1', {
      messages,
      handlers: [shops('owner', [invitation]), (c) => (c.method === 'POST' && c.path === `${BIZ}/b9/staff/st1/accept` ? json(200, member('st1', 'cashier')) : undefined)],
    });
    expect(await screen.findByText(/Green Basket invited you to join as cashier/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Accept Green Basket' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === `${BIZ}/b9/staff/st1/accept`)).toBe(true));
    await waitFor(() => expect(history.location.pathname).toBe('/'));
  });

  it('says so when there is no invitation for this number', async () => {
    renderAt('/invite/zzz', { messages, handlers: [shops('owner', [])] });
    expect(await screen.findByText('No invitation found')).toBeInTheDocument();
  });

  it('shows the used/ended invite error from the API', async () => {
    renderAt('/invite/st1', {
      messages,
      handlers: [shops('owner', [invitation]), (c) => (c.method === 'POST' ? problem(404, 'not_found') : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Accept Green Basket' }));
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
  });
});
