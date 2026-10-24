import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BIZ = '/v1/biz/businesses/b1';
const SETTINGS = `${BIZ}/stores/s1/settings`;

function shops(role: string, status = 'verified') {
  return (c: Call) =>
    c.method === 'GET' && c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status, brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
      : undefined;
}

const STORE = {
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
};

function settings(over: Record<string, unknown> = {}) {
  return {
    hours: { weekly: { mon: [{ open: '09:00', close: '21:00' }] }, holidays: [] },
    closedUntil: null,
    acceptsPickup: true,
    acceptsDelivery: false,
    deliveryRadiusM: 3000,
    deliveryFeePaise: 3000,
    deliveryFreeAbovePaise: null,
    deliveryMinOrderPaise: 10000,
    deliveryHours: null,
    cashbackBp: 500,
    autoAccept: false,
    asksMuted: false,
    openNow: true,
    inStoreOnly: false,
    ...over,
  };
}

const KYC = { status: 'unverified', panLast4: '234F', gstinLast4: null, upiIdLast4: null, bankAccountLast4: '6789', bankIfsc: 'HDFC0001234', legalName: 'Sri Lakshmi Stores' };

function data(over: Record<string, unknown> = {}): Handler {
  return (c) => {
    if (c.method !== 'GET') return undefined;
    if (c.path === `${BIZ}/stores/s1`) return json(200, STORE, { etag: '"v2"' });
    if (c.path === SETTINGS) return json(200, settings(over), { etag: '"v5"' });
    if (c.path === `${BIZ}/kyc`) return json(200, KYC);
    return undefined;
  };
}

function patched(over: Record<string, unknown> = {}): Handler {
  return (c) => (c.method === 'PATCH' && c.path === SETTINGS ? json(200, settings(over), { etag: '"v6"' }) : undefined);
}

beforeEach(() => {
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
});
afterEach(() => vi.unstubAllGlobals());

describe('B-14 settings by role', () => {
  it('cashier gets the no-access state and nothing is fetched for the store', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('cashier'), data()] });
    expect(await screen.findByText('Settings are for owners and managers')).toBeInTheDocument();
    expect(calls.some((c) => c.path.includes('/settings') && c.path !== '/settings')).toBe(false);
    expect(calls.some((c) => c.path.endsWith('/kyc'))).toBe(false);
  });

  it('owner sees the payout summary and the edit form', async () => {
    renderAt('/settings', { messages, handlers: [shops('owner'), data()] });
    expect(await screen.findByText('Saved, ending 234F')).toBeInTheDocument();
    expect(screen.getByText(/Saved, ending 6789/)).toBeInTheDocument();
    expect(screen.getByLabelText('Bank account number')).toBeInTheDocument();
    expect(screen.queryByText('Only the owner can change these. You see the last digits only.')).not.toBeInTheDocument();
  });

  it('manager sees payout masked, with no form, and still sees the asks switch', async () => {
    renderAt('/settings', { messages, handlers: [shops('manager'), data()] });
    expect(await screen.findByText('Saved, ending 234F')).toBeInTheDocument();
    expect(screen.getByText('Only the owner can change these. You see the last digits only.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Bank account number')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('PAN')).not.toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Take asks from customers' })).toBeInTheDocument();
  });

  it('owner saves a PAN with PUT to the kyc route, sending only the filled field', async () => {
    const { calls } = renderAt('/settings', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'PUT' && c.path === `${BIZ}/kyc` ? json(200, { ...KYC, panLast4: '999Z' }) : undefined), data()],
    });
    const pan = await screen.findByLabelText('PAN');
    const section = pan.closest('section') as HTMLElement;
    fireEvent.change(pan, { target: { value: 'ABCDE1234F' } });
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ pan: 'ABCDE1234F' });
  });

  it('owner: a malformed PAN is refused before anything is sent', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('owner'), data()] });
    const pan = await screen.findByLabelText('PAN');
    const section = pan.closest('section') as HTMLElement;
    fireEvent.change(pan, { target: { value: 'nope' } });
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    expect(await within(section).findByText('This does not look right. Check it and try again.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PUT')).toBe(false);
  });
});

describe('B-14 settings saves', () => {
  it('hours: PATCH carries If-Match and only hours', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('owner'), patched(), data()] });
    const section = (await screen.findByRole('heading', { name: 'Opening hours' })).closest('section') as HTMLElement;
    fireEvent.click(within(section).getAllByRole('button', { name: 'Use Monday for every day' })[0] as HTMLElement);
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch).toBeDefined();
    expect(patch?.headers.get('if-match')).toBe('"v5"');
    expect(Object.keys(patch?.body as object)).toEqual(['hours']);
  });

  it('delivery: turning delivery on shows the terms and sends only what changed', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('owner'), patched({ acceptsDelivery: true }), data()] });
    const section = (await screen.findByRole('heading', { name: 'Pickup and delivery' })).closest('section') as HTMLElement;
    expect(within(section).queryByLabelText('Delivery radius (km)')).not.toBeInTheDocument();
    fireEvent.click(within(section).getByRole('checkbox', { name: 'Deliver to customers' }));
    fireEvent.change(within(section).getByLabelText('Delivery radius (km)'), { target: { value: '5' } });
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch).toBeDefined();
    expect(patch?.body).toEqual({ acceptsDelivery: true, deliveryRadiusM: 5000 });
    expect(patch?.headers.get('if-match')).toBe('"v5"');
  });

  it('delivery: a radius over 20 km is refused before anything is sent', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('owner'), patched(), data({ acceptsDelivery: true })] });
    const section = (await screen.findByRole('heading', { name: 'Pickup and delivery' })).closest('section') as HTMLElement;
    fireEvent.change(within(section).getByLabelText('Delivery radius (km)'), { target: { value: '25' } });
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    expect(await within(section).findByText('Enter a distance from 0.1 to 20 km.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false);
  });

  it('D-41: both off shows the in-store-only banner', async () => {
    renderAt('/settings', { messages, handlers: [shops('owner'), data()] });
    const section = (await screen.findByRole('heading', { name: 'Pickup and delivery' })).closest('section') as HTMLElement;
    expect(within(section).queryByText('In-store only')).not.toBeInTheDocument();
    fireEvent.click(within(section).getByRole('checkbox', { name: 'Customers can pick up from the store' }));
    expect(within(section).getByText('In-store only')).toBeInTheDocument();
  });

  it('rewards: cashback example follows the choice (floor of base × bp / 1 000 000) and saves in bp', async () => {
    const { calls } = renderAt('/settings', { messages, handlers: [shops('manager'), patched({ cashbackBp: 750 }), data()] });
    const section = (await screen.findByRole('heading', { name: 'Rewards and orders' })).closest('section') as HTMLElement;
    // 5% of ₹1,000 = 50 credits at 500 bp.
    expect(within(section).getByText(/earns 50 credits/)).toBeInTheDocument();
    fireEvent.change(within(section).getByLabelText('Cashback on bills'), { target: { value: '750' } });
    expect(within(section).getByText(/earns 75 credits/)).toBeInTheDocument();
    fireEvent.click(within(section).getByRole('checkbox', { name: 'Take asks from customers' }));
    fireEvent.click(within(section).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ cashbackBp: 750, asksMuted: true });
  });
});
