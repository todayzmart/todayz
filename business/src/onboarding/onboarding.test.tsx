import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';

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
    referralCode: 'SRI1',
    myRole: 'owner',
    submittedAt: null,
    verifiedAt: null,
    infoRequest: null,
    createdAt: '2026-10-01T00:00:00.000Z',
    ...over,
  };
}

/** The reads the wizard does once a business exists. */
function reads(biz: Record<string, unknown> = {}, docs: unknown[] = []): Handler {
  return (c) => {
    if (c.method !== 'GET') return undefined;
    if (c.path === `${BIZ}/b1`) return json(200, business(biz), { etag: '"v1"' });
    if (c.path === `${BIZ}/b1/stores`) return json(200, { items: [], nextCursor: null });
    if (c.path === `${BIZ}/b1/kyc`) return json(200, { status: 'missing', panLast4: null, gstinLast4: null, upiIdLast4: null, bankAccountLast4: null });
    if (c.path === `${BIZ}/b1/documents`) return json(200, { items: docs });
    return undefined;
  };
}

let globalFetch: ReturnType<typeof vi.fn>;
beforeEach(() => {
  globalFetch = vi.fn(() => Promise.resolve(new Response(null, { status: 200 })));
  vi.stubGlobal('fetch', globalFetch);
});
afterEach(() => vi.unstubAllGlobals());

describe('B-02 onboarding by role', () => {
  it('a manager is told only the owner can set up the shop', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('manager'), reads()] });
    expect(await screen.findByText('Only the owner can set up the shop')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send for review' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/documents') || c.path.endsWith('/kyc'))).toBe(false);
  });

  it('a cashier is told the same', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('cashier'), reads()] });
    expect(await screen.findByText('Only the owner can set up the shop')).toBeInTheDocument();
  });

  it('a new owner creates the business with an Idempotency-Key and gets the next steps', async () => {
    let created = false;
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [
        (c) => (c.method === 'POST' && c.path === BIZ ? ((created = true), json(201, business())) : undefined),
        (c) => (c.method === 'GET' && c.path === BIZ ? json(200, { items: created ? [{ id: 'b1', status: 'draft', brandName: 'Sri Lakshmi', role: 'owner', stores: [] }] : [], invitations: [], nextCursor: null }) : undefined),
        reads(),
      ],
    });
    fireEvent.change(await screen.findByLabelText('Shop name'), { target: { value: '  Sri Lakshmi ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === BIZ)).toBe(true));
    const post = calls.find((c) => c.method === 'POST' && c.path === BIZ);
    expect(post).toBeDefined();
    expect(post?.body).toEqual({ brandName: 'Sri Lakshmi' });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByRole('heading', { name: 'Step 5: Documents' })).toBeInTheDocument();
  });

  it('asks for the shop name before sending anything', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops(null)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Enter your shop name.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('send for review lists what the server says is missing, in the shop’s words', async () => {
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
        reads(),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Send for review' }));
    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Some required details are missing.')).toBeInTheDocument();
    expect(within(alert).getByText('What you sell')).toBeInTheDocument();
    expect(within(alert).getByText('Pincode')).toBeInTheDocument();
    expect(within(alert).getByText('Map location')).toBeInTheDocument();
    expect(within(alert).getByText('Some other detail')).toBeInTheDocument();
  });

  it('a successful submit carries an Idempotency-Key', async () => {
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/submit` ? json(200, business({ status: 'submitted' })) : undefined), reads()],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Send for review' }));
    await waitFor(() => expect(calls.some((c) => c.path === `${BIZ}/b1/submit`)).toBe(true));
    expect(calls.find((c) => c.path === `${BIZ}/b1/submit`)?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('needs_info shows the requested fields and the reviewer’s note', async () => {
    renderAt('/onboarding', {
      messages,
      handlers: [shops('owner', 'needs_info'), reads({ status: 'needs_info', infoRequest: { fields: ['pan', 'documents'], note: 'The PAN photo is blurry.' } })],
    });
    expect(await screen.findByText('We need a little more from you')).toBeInTheDocument();
    const banner = screen.getByText('Please fix these and send for review again.').parentElement as HTMLElement;
    expect(within(banner).getByText('PAN')).toBeInTheDocument();
    expect(within(banner).getByText('Documents')).toBeInTheDocument();
    expect(within(banner).getByText('The PAN photo is blurry.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Send for review' })).toBeInTheDocument();
  });

  it('once submitted the owner can still read, but not send again or add documents', async () => {
    renderAt('/onboarding', { messages, handlers: [shops('owner', 'submitted'), reads({ status: 'submitted' })] });
    expect(await screen.findByText('Your shop is in review')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send for review' })).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Add PAN card')).not.toBeInTheDocument();
  });
});

describe('B-02 documents', () => {
  const upload = {
    document: { id: 'd1', kind: 'pan', contentType: 'image/png', storeId: null, status: 'pending', rejectReason: null, createdAt: '2026-10-24T00:00:00.000Z' },
    upload: { url: 'https://s3.test/put?sig=x', method: 'PUT', headers: { 'Content-Type': 'image/png', 'Content-Length': '4' } },
  };
  const png = () => new File([new Uint8Array([1, 2, 3, 4])], 'pan.png', { type: 'image/png' });

  it('registers the file, then PUTs it with only the signed Content-Type', async () => {
    const { calls } = renderAt('/onboarding', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/documents` ? json(201, upload) : undefined), reads()],
    });
    const input = await screen.findByLabelText('Add PAN card');
    fireEvent.change(input, { target: { files: [png()] } });
    await waitFor(() => expect(globalFetch).toHaveBeenCalled());
    const post = calls.find((c) => c.method === 'POST' && c.path === `${BIZ}/b1/documents`);
    expect(post).toBeDefined();
    expect(post?.body).toEqual({ kind: 'pan', contentType: 'image/png', sizeBytes: 4 });
    expect(post?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    const [url, init] = globalFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://s3.test/put?sig=x');
    expect(init.method).toBe('PUT');
    expect(init.headers).toEqual({ 'Content-Type': 'image/png' });
    expect(await screen.findByText('PAN card added')).toBeInTheDocument();
  });

  it('refuses a wrong type or an empty file before calling the API', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), reads()] });
    const input = await screen.findByLabelText('Add PAN card');
    fireEvent.change(input, { target: { files: [new File(['x'], 'a.exe', { type: 'application/x-msdownload' })] } });
    expect(await screen.findByText('Use a JPG, PNG or WebP photo, or a PDF.')).toBeInTheDocument();
    fireEvent.change(input, { target: { files: [new File([], 'a.png', { type: 'image/png' })] } });
    expect(await screen.findByText('This file is empty.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('refuses a file over 10 MB', async () => {
    const { calls } = renderAt('/onboarding', { messages, handlers: [shops('owner'), reads()] });
    const input = await screen.findByLabelText('Add PAN card');
    const big = new File(['x'], 'big.pdf', { type: 'application/pdf' });
    Object.defineProperty(big, 'size', { value: 10 * 1024 * 1024 + 1 });
    fireEvent.change(input, { target: { files: [big] } });
    expect(await screen.findByText('This file is bigger than 10 MB.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('says so when the storage upload fails', async () => {
    globalFetch.mockImplementation(() => Promise.resolve(new Response(null, { status: 403 })));
    renderAt('/onboarding', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'POST' && c.path === `${BIZ}/b1/documents` ? json(201, upload) : undefined), reads()],
    });
    fireEvent.change(await screen.findByLabelText('Add PAN card'), { target: { files: [png()] } });
    expect(await screen.findByText('The file did not upload. Check your connection and add it again.')).toBeInTheDocument();
  });
});
