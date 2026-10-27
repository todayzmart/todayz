import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';
import { firstOpen, stepFrom, stepStates, type StepId } from './steps';

const messages = asMessages(en);
const BIZ = '/v1/biz/businesses';

function shops(role: string | null, status = 'draft'): Handler {
  return (c: Call) =>
    c.method === 'GET' && c.path === BIZ
      ? json(200, {
          items: role ? [{ id: 'b1', status, brandName: 'Sri Lakshmi', role, stores: [] }] : [],
          invitations: [],
          nextCursor: null,
        })
      : undefined;
}

function business(over: Record<string, unknown> = {}) {
  return {
    id: 'b1',
    brandName: 'Sri Lakshmi',
    legalName: null,
    categoryId: null,
    status: 'draft',
    checkStatus: 'unchecked',
    referralCode: 'SRI1',
    myRole: 'owner',
    submittedAt: null,
    verifiedAt: null,
    infoRequest: null,
    createdAt: '2026-10-01T00:00:00.000Z',
    ...over,
  };
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
  status: 'draft',
  openNow: false,
  createdAt: '2026-10-01T00:00:00.000Z',
};

const SETTINGS = {
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
  openNow: false,
  inStoreOnly: false,
};

interface Reads {
  biz?: Record<string, unknown> | (() => Record<string, unknown>);
  docs?: unknown[];
  /** The first store (with hours set) exists. */
  store?: boolean;
}

/** The reads the flow does once a business exists. */
function reads({ biz = {}, docs = [], store = false }: Reads = {}): Handler {
  return (c) => {
    if (c.method !== 'GET') return undefined;
    if (c.path === `${BIZ}/b1`) return json(200, business(typeof biz === 'function' ? biz() : biz), { etag: '"v1"' });
    if (c.path === `${BIZ}/b1/stores`) return json(200, { items: store ? [STORE] : [], nextCursor: null });
    if (c.path === `${BIZ}/b1/stores/s1/settings`) return json(200, SETTINGS, { etag: '"v1"' });
    if (c.path === `${BIZ}/b1/kyc`) return json(200, { status: 'missing', panLast4: null, gstinLast4: null, upiIdLast4: null, bankAccountLast4: null });
    if (c.path === `${BIZ}/b1/documents`) return json(200, { items: docs });
    if (c.path === '/v1/discover/categories')
      return json(200, {
        items: [
          { id: 'c1', name: 'Grocery', slug: 'grocery', parentId: null },
          { id: 'c2', name: 'Pharmacy', slug: 'pharmacy', parentId: null },
        ],
      });
    return undefined;
  };
}

/** Everything needed to go live is in: store, hours, category. */
const ready = (over: Record<string, unknown> = {}) => reads({ biz: { categoryId: 'c1', ...over }, store: true });

const rail = () => screen.getByRole('navigation', { name: 'Setup steps' });
const railStep = (name: RegExp) => within(rail()).getByRole('button', { name });
/** The Go live button in the step card, not the rail's step of the same name. */
const goLive = () => waitFor(() => within(screen.getByRole('main')).getByRole('button', { name: 'Go live' }));

let globalFetch: ReturnType<typeof vi.fn>;
beforeEach(() => {
  globalFetch = vi.fn(() => Promise.resolve(new Response(null, { status: 200 })));
  vi.stubGlobal('fetch', globalFetch);
});
afterEach(() => vi.unstubAllGlobals());

describe('B-02 steps', () => {
  const none: Record<StepId, boolean> = { business: false, store: false, hours: false, category: false, photos: false, payout: false, live: false };

  it('opens the first needed step still to do, skipping photos and the optional payout step', () => {
    expect(firstOpen(stepStates(none))).toBe('business');
    expect(firstOpen(stepStates({ ...none, business: true }))).toBe('store');
    expect(firstOpen(stepStates({ ...none, business: true, store: true, hours: true }))).toBe('category');
    expect(firstOpen(stepStates({ ...none, business: true, store: true, hours: true, category: true }))).toBe('live');
  });

  it('hours wait for a store; Back and Next step over what cannot be opened', () => {
    const s = stepStates({ ...none, business: true });
    expect(s.find((x) => x.id === 'hours')?.reachable).toBe(false);
    expect(stepFrom(s, 'store', 1)).toBe('category');
    expect(stepFrom(s, 'category', -1)).toBe('store');
    expect(stepFrom(s, 'business', -1)).toBeNull();
    expect(stepFrom(s, 'live', 1)).toBeNull();
  });
});

describe('B-02 onboarding by role', () => {
  it('a manager is told only the owner can set up the shop', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('manager'), reads()] });
    expect(await screen.findByText('Only the owner can set up the shop')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Go live' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/documents') || c.path.endsWith('/kyc'))).toBe(false);
  });

  it('a cashier is told the same', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('cashier'), reads()] });
    expect(await screen.findByText('Only the owner can set up the shop')).toBeInTheDocument();
  });
});

describe('B-02 the flow', () => {
  it('is drawn without the shell: its own green rail and main, no app sidebar', async () => {
    renderAt('/onboarding', { messages, handlers: [shops(null)] });
    expect(await screen.findByRole('heading', { name: 'Set up your shop' })).toBeInTheDocument();
    expect(screen.getAllByRole('main')).toHaveLength(1);
    expect(screen.queryByRole('link', { name: 'Orders' })).not.toBeInTheDocument();
    // A new signup has no shop yet, so there is no dashboard to go to.
    expect(screen.queryByRole('link', { name: 'Go to dashboard' })).not.toBeInTheDocument();
  });

  it('a new owner sees seven steps with only the first open, creates the business with an Idempotency-Key and moves on to the address', async () => {
    let created = false;
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [
        (c) => (c.method === 'POST' && c.path === BIZ ? ((created = true), json(201, business())) : undefined),
        (c) => (c.method === 'GET' && c.path === BIZ ? json(200, { items: created ? [{ id: 'b1', status: 'draft', brandName: 'Sri Lakshmi', role: 'owner', stores: [] }] : [], invitations: [], nextCursor: null }) : undefined),
        reads(),
      ],
    });
    expect(await screen.findByRole('heading', { name: 'Shop name' })).toBeInTheDocument();
    const steps = within(rail()).getAllByRole('button');
    expect(steps).toHaveLength(7);
    expect(steps[0]).toHaveAttribute('aria-current', 'step');
    expect(steps.slice(1).every((b) => (b as HTMLButtonElement).disabled)).toBe(true);
    expect(screen.getByText('Step 1 of 7')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('textbox', { name: 'Shop name' }), { target: { value: '  Sri Lakshmi ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === BIZ)).toBe(true));
    const post = calls.find((c) => c.method === 'POST' && c.path === BIZ);
    expect(post?.body).toEqual({ brandName: 'Sri Lakshmi' });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByRole('heading', { name: 'Address and map pin' })).toBeInTheDocument();
    expect(railStep(/Shop name/)).toHaveTextContent('Done');
    expect(railStep(/Opening hours/)).toBeDisabled();
    expect(railStep(/GST, PAN and payouts/)).toHaveTextContent('Optional');
  });

  it('asks for the shop name before sending anything', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops(null)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Enter your shop name.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('a category is one tap: it saves with If-Match and the step is done', async () => {
    let cat: string | null = null;
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.method === 'PATCH' && c.path === `${BIZ}/b1` ? ((cat = 'c2'), json(200, business({ categoryId: 'c2' }), { etag: '"v2"' })) : undefined),
        reads({ biz: () => ({ categoryId: cat }), store: true }),
      ],
    });
    expect(await screen.findByRole('heading', { name: 'What you sell' })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('radio', { name: 'Pharmacy' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch?.body).toEqual({ categoryId: 'c2' });
    expect(patch?.headers.get('if-match')).toBe('"v1"');
    await waitFor(() => expect(railStep(/What you sell/)).toHaveTextContent('Done'));
  });

  it('with everything in, it opens Go live and lists the recommended photos still to add', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('owner'), ready()] });
    expect(await screen.findByRole('heading', { name: 'Go live' })).toBeInTheDocument();
    expect(screen.getByText('4 of 7 steps done')).toBeInTheDocument();
    const todo = screen.getByRole('button', { name: /Shop photos · Recommended/ });
    fireEvent.click(todo);
    expect(await screen.findByRole('heading', { name: 'Shop photos' })).toBeInTheDocument();
    expect(screen.getByLabelText('Add Photo of the shop front')).toBeInTheDocument();
    expect(screen.queryByLabelText('Add PAN card')).not.toBeInTheDocument();
  });

  it('Go live lists what the server says is missing, in the shop’s words', async () => {
    renderAt('/onboarding', {
      messages,
      handlers: [
        shops('owner'),
        (c) =>
          c.method === 'POST' && c.path === `${BIZ}/b1/submit`
            ? json(422, {
                type: 'about:blank#submit_incomplete',
                title: 'incomplete',
                status: 422,
                code: 'submit_incomplete',
                fields: { categoryId: 'required', 'stores[0].pincode': 'required', 'stores[0].location': 'required', 'stores[0].mystery': 'required' },
              })
            : undefined,
        ready(),
      ],
    });
    fireEvent.click(await goLive());
    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Some required details are missing.')).toBeInTheDocument();
    expect(within(alert).getByText('What you sell')).toBeInTheDocument();
    expect(within(alert).getByText('Pincode')).toBeInTheDocument();
    expect(within(alert).getByText('Map location')).toBeInTheDocument();
    expect(within(alert).getByText('Some other detail')).toBeInTheDocument();
  });

  it('Go live carries an Idempotency-Key; the shop is live at once and told payouts wait for our check', async () => {
    let status = 'draft';
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/submit` ? ((status = 'verified'), json(200, business({ status, categoryId: 'c1' }))) : undefined),
        reads({ biz: () => ({ status, categoryId: 'c1' }), store: true }),
      ],
    });
    fireEvent.click(await goLive());
    await waitFor(() => expect(calls.some((c) => c.path === `${BIZ}/b1/submit`)).toBe(true));
    expect(calls.find((c) => c.path === `${BIZ}/b1/submit`)?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByText('Customers can find your live stores. Payouts start after our team checks your shop.')).toBeInTheDocument();
    expect(within(screen.getByRole('main')).queryByRole('button', { name: 'Go live' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Add your products' })).toHaveAttribute('href', '/stock');
    expect(railStep(/Go live/)).toHaveTextContent('Done');
  });

  it('a checked shop is told payouts go out as usual', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('owner', 'verified'), ready({ status: 'verified', checkStatus: 'checked' })] });
    expect(await screen.findByText('Your shop is live and checked')).toBeInTheDocument();
    expect(within(screen.getByRole('complementary')).getByRole('link', { name: 'Go to dashboard' })).toHaveAttribute('href', '/');
  });

  it('a live shop can still add photos and papers, but not change its category', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('owner', 'verified'), ready({ status: 'verified' })] });
    fireEvent.click(await within(await screen.findByRole('navigation', { name: 'Setup steps' })).findByRole('button', { name: /What you sell/ }));
    expect(await screen.findByRole('radio', { name: 'Grocery' })).toBeDisabled();
    fireEvent.click(railStep(/Shop photos/));
    expect(await screen.findByLabelText('Add Photo of the shop front')).toBeInTheDocument();
  });

  it('a rejected shop is told it was taken down and can add nothing', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('owner', 'rejected'), ready({ status: 'rejected', checkStatus: 'rejected' })] });
    expect(await screen.findByText('Your shop has been taken down')).toBeInTheDocument();
    fireEvent.click(railStep(/Shop photos/));
    expect(await screen.findByRole('heading', { name: 'Shop photos' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Add Photo of the shop front')).not.toBeInTheDocument();
  });

  it('needs_info (sent for review before D-274) shows the requested fields and the note', async () => {
    renderAt('/onboarding', {
      messages,
      handlers: [shops('owner', 'needs_info'), reads({ biz: { status: 'needs_info', infoRequest: { fields: ['pan', 'documents'], note: 'The PAN photo is blurry.' } } })],
    });
    expect(await screen.findByText('We need a little more from you')).toBeInTheDocument();
    const banner = screen.getByText('Please fix these and send for review again.').parentElement as HTMLElement;
    expect(within(banner).getByText('PAN')).toBeInTheDocument();
    expect(within(banner).getByText('Documents')).toBeInTheDocument();
    expect(within(banner).getByText('The PAN photo is blurry.')).toBeInTheDocument();
  });
});

describe('B-02 documents', () => {
  const upload = (kind: string) => ({
    document: { id: 'd1', kind, contentType: 'image/png', storeId: null, status: 'pending', rejectReason: null, createdAt: '2026-10-24T00:00:00.000Z' },
    upload: { url: 'https://s3.test/put?sig=x', method: 'PUT', headers: { 'Content-Type': 'image/png', 'Content-Length': '4' } },
  });
  const accepted = { id: 'd1', kind: 'pan', contentType: 'image/png', storeId: null, status: 'accepted', rejectReason: null, createdAt: '2026-10-24T00:00:00.000Z' };
  const png = () => new File([new Uint8Array([1, 2, 3, 4])], 'pan.png', { type: 'image/png' });
  const docHandlers = (kind: string): Handler => (c) => {
    if (c.method === 'POST' && c.path === `${BIZ}/b1/documents`) return json(201, upload(kind));
    if (c.method === 'POST' && c.path === `${BIZ}/b1/documents/d1/confirm`) return json(200, { ...accepted, kind });
    return undefined;
  };
  const openPayout = async () => {
    await screen.findByRole('navigation', { name: 'Setup steps' });
    fireEvent.click(railStep(/GST, PAN and payouts/));
    return screen.findByLabelText('Add PAN card');
  };

  it('registers the file, PUTs it with only the signed Content-Type, then confirms it (accepted at once)', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), docHandlers('pan'), reads()] });
    fireEvent.change(await openPayout(), { target: { files: [png()] } });
    await waitFor(() => expect(calls.some((c) => c.path === `${BIZ}/b1/documents/d1/confirm`)).toBe(true));
    const post = calls.find((c) => c.method === 'POST' && c.path === `${BIZ}/b1/documents`);
    expect(post?.body).toEqual({ kind: 'pan', contentType: 'image/png', sizeBytes: 4 });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    const [url, init] = globalFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://s3.test/put?sig=x');
    expect(init.method).toBe('PUT');
    expect(init.headers).toEqual({ 'Content-Type': 'image/png' });
    expect(await screen.findByText('PAN card added')).toBeInTheDocument();
  });

  it('a shop front photo marks the photos step done', async () => {
    let docs: unknown[] = [];
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [
        shops('owner'),
        (c) => {
          const r = docHandlers('storefront')(c);
          if (r && c.path.endsWith('/confirm')) docs = [{ ...accepted, kind: 'storefront' }];
          return r;
        },
        (c) => (c.method === 'GET' && c.path === `${BIZ}/b1/documents` ? json(200, { items: docs }) : undefined),
        reads(),
      ],
    });
    await screen.findByRole('navigation', { name: 'Setup steps' });
    fireEvent.click(railStep(/Shop photos/));
    fireEvent.change(await screen.findByLabelText('Add Photo of the shop front'), { target: { files: [png()] } });
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/confirm'))).toBe(true));
    await waitFor(() => expect(railStep(/Shop photos/)).toHaveTextContent('Done'));
    expect(await screen.findByText('Added')).toBeInTheDocument();
  });

  it('refuses a wrong type or an empty file before calling the API', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), reads()] });
    const input = await openPayout();
    fireEvent.change(input, { target: { files: [new File(['x'], 'a.exe', { type: 'application/x-msdownload' })] } });
    expect(await screen.findByText('Use a JPG, PNG or WebP photo, or a PDF.')).toBeInTheDocument();
    fireEvent.change(input, { target: { files: [new File([], 'a.png', { type: 'image/png' })] } });
    expect(await screen.findByText('This file is empty.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('refuses a file over 10 MB', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), reads()] });
    const input = await openPayout();
    const big = new File(['x'], 'big.pdf', { type: 'application/pdf' });
    Object.defineProperty(big, 'size', { value: 10 * 1024 * 1024 + 1 });
    fireEvent.change(input, { target: { files: [big] } });
    expect(await screen.findByText('This file is bigger than 10 MB.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('says so when the storage upload fails, and does not confirm', async () => {
    globalFetch.mockImplementation(() => Promise.resolve(new Response(null, { status: 403 })));
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), docHandlers('pan'), reads()] });
    fireEvent.change(await openPayout(), { target: { files: [png()] } });
    expect(await screen.findByText('The file did not upload. Check your connection and add it again.')).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/confirm'))).toBe(false);
  });
});
