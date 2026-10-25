import type { FieldAgentStats } from '@todayz/api-client/gen/model/fieldAgentStats';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldItem } from '@todayz/api-client/gen/model/fieldItem';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NOW, STORE_ID } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { toE164, toPaise } from './labels';

const BID = '01JF1E1DBUS000000000000B01';
const ITEM_ID = '01JF1E1D1TEM00000000000I01';
const PRODUCT_ID = '01JF1E1DPR0D00000000000P01';
const CAT = '01JCATEG0RY0000000000000C1';
const CHILD = '01JCATEG0RYCH1LD00000000C2';
const AGENT = '01JAGENT00000000000000A001';

function shop(over: Partial<FieldBusiness> = {}): FieldBusiness {
  return {
    id: BID,
    brandName: 'Ganesh Kirana',
    legalName: null,
    categoryId: CAT,
    status: 'draft',
    ownerPhoneMasked: '+91 ••••••3210',
    ownerConfirmed: false,
    onboardedBy: AGENT,
    verifiedAt: null,
    editableUntil: null,
    itemsEditable: true,
    stores: [{ id: STORE_ID, name: 'Ganesh Kirana, Basavanagudi', status: 'draft', locality: 'Basavanagudi', itemCount: 1 }],
    photoCount: 1,
    createdAt: '2026-10-24T05:00:00Z',
    updatedAt: '2026-10-24T05:00:00Z',
    ...over,
  };
}

function item(over: Partial<FieldItem> = {}): FieldItem {
  return {
    id: ITEM_ID,
    storeId: STORE_ID,
    productId: PRODUCT_ID,
    name: 'Toor dal',
    brand: 'Tata Sampann',
    variant: null,
    packSize: 1,
    unit: 'kg',
    pricePaise: 17_500,
    mrpPaise: 18_000,
    productMrpPaise: 18_000,
    onHand: 12,
    isListed: true,
    productStatus: 'active',
    editable: true,
    ...over,
  } as FieldItem;
}

function stats(over: Partial<FieldAgentStats> = {}): FieldAgentStats {
  return {
    agentId: AGENT,
    name: 'Suresh',
    businesses: { total: 4, draft: 1, submitted: 1, needsInfo: 0, verified: 2, rejected: 0, suspended: 0 },
    storesLive: 2,
    itemsAdded: 57,
    inWindow: 2,
    disabled: false,
    firstOrderRate: 0.5,
    weekly: Array.from({ length: 8 }, (_, i) => ({
      weekStart: new Date(Date.UTC(2026, 8, 7 + i * 7)).toISOString().slice(0, 10),
      onboarded: i % 3,
      verified: i % 2,
      itemsAdded: i * 3,
    })),
    ...over,
  };
}

const categories = {
  items: [
    {
      id: CAT,
      slug: 'grocery',
      name: 'Grocery',
      names: {},
      tint: 'mint',
      icon: 'shopping-basket',
      children: [{ id: CHILD, slug: 'dal', name: 'Dals and pulses', names: {}, tint: 'mint', icon: 'bean' }],
    },
  ],
};

/** The field API over one shop, keeping what the screens change. */
function api(start = shop(), items: FieldItem[] = [item()]): Handler {
  let b = start;
  let list = items;
  const base = `/v1/admin/field/businesses/${BID}`;
  return (c) => {
    if (c.path === '/v1/discover/categories') return json(200, categories);
    if (c.path === '/v1/admin/field/businesses' && c.method === 'GET') return json(200, { items: [b], nextCursor: null, serverNow: NOW });
    if (c.path === '/v1/admin/field/businesses' && c.method === 'POST') {
      const body = c.body as { brandName: string };
      b = shop({ brandName: body.brandName, categoryId: null, stores: [], photoCount: 0 });
      return json(201, b);
    }
    if (c.path === base && c.method === 'GET') return json(200, b);
    if (c.path === `${base}/stores/${STORE_ID}/items` && c.method === 'GET')
      return json(200, { items: list, nextCursor: null, itemsEditable: b.itemsEditable, editableUntil: b.editableUntil, serverNow: NOW });
    if (c.path === `${base}/stores/${STORE_ID}/items` && c.method === 'POST') {
      const body = c.body as { productId?: string; pricePaise: number };
      if (body.productId === ITEM_ID) return problem(409, 'conflict');
      const added = item({ id: '01JF1E1D1TEM00000000000I02', name: 'Sona masoori rice', pricePaise: body.pricePaise });
      list = [...list, added];
      return json(201, added);
    }
    if (c.path === `${base}/stores/${STORE_ID}/products`)
      return json(200, {
        items: [
          { id: PRODUCT_ID, name: 'Toor dal', brand: 'Tata Sampann', variant: null, packSize: 1, unit: 'kg', mrpPaise: 18_000, categoryId: CHILD, inStore: true },
          { id: '01JF1E1DPR0D00000000000P02', name: 'Sona masoori rice', brand: 'India Gate', variant: null, packSize: 5, unit: 'kg', mrpPaise: 42_000, categoryId: CHILD, inStore: false },
        ],
      });
    return undefined;
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('field helpers', () => {
  it('reads a 10-digit mobile number however it is typed', () => {
    expect(toE164('98765 43210')).toBe('+919876543210');
    expect(toE164('098765-43210')).toBe('+919876543210');
    expect(toE164('+91 98765 43210')).toBe('+919876543210');
    expect(toE164('12345')).toBeNull();
    expect(toE164('5876543210')).toBeNull();
  });

  it('reads rupees to paise', () => {
    expect(toPaise('45')).toBe(4500);
    expect(toPaise('45.5')).toBe(4550);
    expect(toPaise('0')).toBeNull();
    expect(toPaise('4.555')).toBeNull();
  });
});

describe('A-04 field onboarding', () => {
  it('starts a field agent on their shops, with Add shops and Field agents in the sidebar', async () => {
    const { calls } = renderAt('/', { roles: ['field_agent'], handlers: [api()] });
    const list = await screen.findByRole('list', { name: 'Your shops' });
    expect(within(list).getByRole('link', { name: /Ganesh Kirana/ })).toHaveAttribute('href', `/field/${BID}`);
    const nav = screen.getByRole('navigation');
    expect(within(nav).getByRole('link', { name: 'Add shops' })).toBeInTheDocument();
    expect(within(nav).getByRole('link', { name: 'Field agents' })).toBeInTheDocument();
    expect(calls.some((c) => c.path.startsWith('/v1/admin/pulse'))).toBe(false);
  });

  it('checks the owner number before creating, then opens the wizard on the next step', async () => {
    const { calls, history } = renderAt('/field/new', { roles: ['field_agent'], handlers: [api()] });
    fireEvent.change(await screen.findByLabelText('Shop name'), { target: { value: 'Ganesh Kirana' } });
    fireEvent.change(screen.getByLabelText(/Owner's mobile number/), { target: { value: '12345' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create shop' }));
    expect(await screen.findByText('Enter a 10-digit Indian mobile number.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    fireEvent.change(screen.getByLabelText(/Owner's mobile number/), { target: { value: '98765 43210' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create shop' }));
    await waitFor(() => expect(history.location.pathname).toBe(`/field/${BID}`));
    const post = calls.find((c) => c.method === 'POST' && c.path === '/v1/admin/field/businesses');
    expect(post?.body).toMatchObject({ brandName: 'Ganesh Kirana', ownerPhone: '+919876543210' });
    expect(post?.headers.get('Idempotency-Key')).toBeTruthy();
    // No category yet: the wizard opens on Details.
    expect(await screen.findByRole('button', { name: /Details/, current: 'step' })).toBeInTheDocument();
  });

  it('sends the owner a code, says how many tries are left, and confirms', async () => {
    let tries = 0;
    const owner: Handler = (c) => {
      if (c.path.endsWith('/owner-confirm')) return json(200, { phoneMasked: '+91 ••••••3210', expiresAt: '2026-10-25T06:40:00Z', resendAfterSeconds: 30 });
      if (c.path.endsWith('/owner-confirm/verify')) {
        tries += 1;
        if (tries === 1) return json(422, { type: 'about:blank', title: 'x', status: 422, code: 'code_invalid', extensions: { attemptsLeft: 4 } });
        return json(200, shop({ status: 'submitted', ownerConfirmed: true }));
      }
      return undefined;
    };
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [owner, api()] });
    // Every step but the owner is done, so the wizard opens there.
    fireEvent.click(await screen.findByRole('button', { name: 'Send code to owner' }));
    expect(await screen.findByText(/Code sent to \+91 ••••••3210/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Send a new code in 30 seconds/ })).toBeDisabled();

    const group = screen.getByRole('group', { name: "Owner's code" });
    expect(within(group).getAllByRole('textbox')).toHaveLength(6);
    fireEvent.change(within(group).getByRole('textbox', { name: "Owner's code" }), { target: { value: '111111' } });
    expect(await screen.findByText('That code is not right. 4 tries left.')).toBeInTheDocument();

    fireEvent.change(within(group).getByRole('textbox', { name: "Owner's code" }), { target: { value: '222222' } });
    expect(await screen.findByText('Owner confirmed. The shop is sent for review.')).toBeInTheDocument();
    expect(await screen.findByText('The owner has confirmed')).toBeInTheDocument();
  });

  it('says when the owner number cannot own a shop', async () => {
    const refused: Handler = (c) =>
      c.path.endsWith('/owner-confirm')
        ? json(422, { type: 'about:blank', title: 'x', status: 422, code: 'validation_failed', fields: { 'body.ownerPhone': 'this number cannot own a business' } })
        : undefined;
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [refused, api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Send code to owner' }));
    expect(await screen.findByText('This number cannot own a shop on TodayZ. Use another number for the owner.')).toBeInTheDocument();
  });

  it('points to the missing store details when the code cannot be sent yet', async () => {
    const incomplete: Handler = (c) =>
      c.path.endsWith('/owner-confirm')
        ? json(409, { type: 'about:blank', title: 'x', status: 409, code: 'submit_incomplete', fields: { 'stores[0].phone': 'required' } })
        : undefined;
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [incomplete, api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Send code to owner' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Finish the store' }));
    expect(await screen.findByRole('button', { name: /Store/, current: 'step' })).toBeInTheDocument();
  });

  it('adds a product from the catalogue with a price, and says when it is already listed', async () => {
    const { calls } = renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: /Products/ }));
    expect(await screen.findByText('Toor dal')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Find a product' }), { target: { value: 'dal' } });
    const matches = await screen.findByRole('list', { name: 'Matching products' });
    expect(await within(matches).findByText('Already listed')).toBeInTheDocument();
    fireEvent.click(within(matches).getByRole('button', { name: 'Add Sona masoori rice' }));
    const form = await screen.findByRole('form', { name: 'Add Sona masoori rice' });
    fireEvent.change(within(form).getByLabelText('Price'), { target: { value: '399' } });
    fireEvent.change(within(form).getByLabelText('Opening stock'), { target: { value: '6' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('Sona masoori rice added')).toBeInTheDocument();
    const post = calls.find((c) => c.method === 'POST' && c.path.endsWith('/items'));
    expect(post?.body).toEqual({ productId: '01JF1E1DPR0D00000000000P02', pricePaise: 39_900, openingStock: 6 });
  });

  it('shows items read-only once the edit window has closed', async () => {
    const closed = shop({ status: 'verified', ownerConfirmed: true, verifiedAt: '2026-09-01T05:00:00Z', editableUntil: '2026-10-01T05:00:00Z', itemsEditable: false });
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [api(closed)] });
    expect(await screen.findByText(/The edit window closed on/)).toBeInTheDocument();
    expect(await screen.findByText('Toor dal')).toBeInTheDocument();
    expect(screen.getByText('The edit window for this shop has closed, so this is read-only.')).toBeInTheDocument();
    expect(screen.queryByRole('searchbox', { name: 'Find a product' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Edit the price/ })).not.toBeInTheDocument();
  });

  it('offers Edit only on the items the agent added (D-232)', async () => {
    const shopOwn = item({ id: '01JF1E1D1TEM00000000000I03', name: 'Moong dal', editable: false });
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [api(shop(), [item(), shopOwn])] });
    fireEvent.click(await screen.findByRole('button', { name: /Products/ }));
    expect(await screen.findByText('Moong dal')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Edit the price of Toor dal' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Edit the price of Moong dal' })).not.toBeInTheDocument();
    expect(screen.getByText('Added by someone else')).toBeInTheDocument();
  });

  it('refuses a large photo before asking for an upload', async () => {
    const { calls } = renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: /Photos/ }));
    const input = await screen.findByTestId('field-photo-input');
    const big = new File([new Uint8Array(11 * 1024 * 1024)], 'front.jpg', { type: 'image/jpeg' });
    fireEvent.change(input, { target: { files: [big] } });
    expect(await screen.findByText('The photo must be under 10 MB.')).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/documents'))).toBe(false);
  });

  const DOC = '01JD0C000000000000000000D9';
  // The photo is registered (pending), PUT, then confirmed with this status.
  const docs =
    (confirmed: number): Handler =>
    (c) => {
      if (c.path.endsWith(`/documents/${DOC}/confirm`))
        return confirmed === 200
          ? json(200, { id: DOC, kind: 'storefront', contentType: 'image/jpeg', storeId: STORE_ID, status: 'uploaded', createdAt: NOW })
          : problem(confirmed, 'validation_failed');
      if (c.path.endsWith('/documents'))
        return json(201, {
          document: { id: DOC, kind: 'storefront', contentType: 'image/jpeg', storeId: STORE_ID, status: 'pending', createdAt: NOW },
          upload: { url: 'https://s3.test/put', method: 'PUT', headers: { 'Content-Type': 'image/jpeg' }, expiresAt: NOW },
        });
      return undefined;
    };

  it('uploads a photo to the presigned URL and confirms it', async () => {
    const put = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', put);
    const { calls } = renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [docs(200), api()] });
    fireEvent.click(await screen.findByRole('button', { name: /Photos/ }));
    fireEvent.change(await screen.findByTestId('field-photo-input'), {
      target: { files: [new File(['jpeg'], 'front.jpg', { type: 'image/jpeg' })] },
    });
    expect(await screen.findByText('Photo added')).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith('/documents'))?.body).toMatchObject({ kind: 'storefront', contentType: 'image/jpeg', sizeBytes: 4, storeId: STORE_ID });
    expect(put).toHaveBeenCalledWith('https://s3.test/put', expect.objectContaining({ method: 'PUT' }));
    expect(calls.some((c) => c.method === 'POST' && c.path.endsWith(`/documents/${DOC}/confirm`))).toBe(true);
  });

  it('says the upload failed when the API cannot find the stored photo', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 200 })));
    renderAt(`/field/${BID}`, { roles: ['field_agent'], handlers: [docs(422), api()] });
    fireEvent.click(await screen.findByRole('button', { name: /Photos/ }));
    fireEvent.change(await screen.findByTestId('field-photo-input'), {
      target: { files: [new File(['jpeg'], 'front.jpg', { type: 'image/jpeg' })] },
    });
    expect(await screen.findByText('The photo did not upload. Check the connection and try again.')).toBeInTheDocument();
  });
});

describe('A-15 field agents', () => {
  it('shows a field agent only their own numbers', async () => {
    const mine: Handler = (c) => (c.path === '/v1/admin/field/me/stats' ? json(200, { stats: stats(), serverNow: NOW }) : undefined);
    const { calls } = renderAt('/field-agents', { roles: ['field_agent'], handlers: [mine] });
    const tiles = await screen.findByRole('list', { name: 'Your numbers' });
    expect(within(tiles).getByText('57')).toBeInTheDocument();
    expect(within(tiles).getByText('50%')).toBeInTheDocument();
    expect(within(screen.getByRole('list', { name: 'Shops onboarded per week' })).getAllByRole('listitem')).toHaveLength(8);
    expect(calls.some((c) => c.path === '/v1/admin/field-agents')).toBe(false);
  });

  it('shows admins every agent, disabled ones marked, and their trend', async () => {
    const all: Handler = (c) =>
      c.path === '/v1/admin/field-agents'
        ? json(200, { items: [stats(), stats({ agentId: '01JAGENT00000000000000A002', name: 'Divya', firstOrderRate: null, disabled: true })], serverNow: NOW })
        : undefined;
    const { calls } = renderAt('/field-agents', { roles: ['admin'], handlers: [all] });
    const table = await screen.findByRole('table', { name: 'Field agents' });
    expect(await within(table).findByText('Divya')).toBeInTheDocument();
    expect(within(table).getAllByText('Disabled')).toHaveLength(1);
    const row = within(table).getByText('Divya').closest('tr');
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText('Disabled')).toBeInTheDocument();
    fireEvent.click(within(table).getByRole('button', { name: 'Show the weekly trend of Divya' }));
    expect(await screen.findByRole('heading', { name: 'Weekly trend: Divya' })).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/field/me/stats')).toBe(false);
  });
});
