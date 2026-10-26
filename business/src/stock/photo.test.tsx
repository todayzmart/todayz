import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';
const CATEGORY = { id: 'c1', slug: 'grocery', name: 'Grocery', names: {}, tint: 'mint', icon: 'shopping-basket', children: [] };

// The presigned PUT goes to the storage host through the page's own fetch, not the API session.
let storage: ReturnType<typeof vi.fn>;
beforeEach(() => {
  storage = vi.fn(() => Promise.resolve(new Response(null, { status: 200 })));
  vi.stubGlobal('fetch', storage);
});
afterEach(() => vi.unstubAllGlobals());

const shops: Handler = (c) =>
  c.path === '/v1/biz/businesses'
    ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'manager', stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
    : undefined;

function item(id: string, name: string, over: Record<string, unknown> = {}) {
  return {
    id,
    storeId: 's1',
    productId: `p-${id}`,
    name,
    productName: name,
    localName: null,
    brand: null,
    variant: null,
    packSize: 1,
    unit: 'kg',
    categoryId: 'c1',
    barcodes: [],
    pricePaise: 5800,
    mrpPaise: null,
    productMrpPaise: null,
    onHand: 12,
    reserved: 0,
    available: 12,
    lowStockAt: null,
    lowStock: false,
    isListed: true,
    stockUpdatedAt: '2026-10-24T08:00:00.000Z',
    stale: false,
    imageKey: null,
    photo: null,
    createdAt: '2026-10-01T08:00:00.000Z',
    updatedAt: '2026-10-24T08:00:00.000Z',
    ...over,
  };
}

const reads = (items: ReturnType<typeof item>[]): Handler[] => [
  (c) => (c.method === 'GET' && c.path === `${BASE}/items` ? json(200, { items, nextCursor: null }) : undefined),
  (c) => {
    const found = items.find((i) => c.path === `${BASE}/items/${i.id}`);
    return c.method === 'GET' && found ? json(200, found, { ETag: '"v1"' }) : undefined;
  },
  (c) => (c.method === 'GET' && (c.path === `${BASE}/batches` || c.path.endsWith('/history')) ? json(200, { items: [], nextCursor: null }) : undefined),
  (c) => (c.method === 'GET' && c.path === '/v1/discover/categories' ? json(200, { items: [CATEGORY], nextCursor: null }) : undefined),
  (c) => (c.method === 'GET' && c.path === '/v1/biz/businesses/b1/products/search' ? json(200, { items: [], nextCursor: null }) : undefined),
];

const created = { id: 'p9', name: 'Jaggery', brand: null, variant: null, packSize: 500, unit: 'g', mrpPaise: null, categoryId: 'c1', barcodes: [], status: 'pending_review' };
const upload = {
  photo: { id: 'ph1', productId: 'p9', status: 'uploading', contentType: 'image/png', previewUrl: null, rejectReason: null, createdAt: '2026-10-26T08:00:00.000Z' },
  upload: { url: 'https://s3.test/put?sig=x', method: 'PUT', headers: { 'Content-Type': 'image/png', 'Content-Length': '4' }, expiresAt: '2026-10-26T08:15:00.000Z' },
};

async function openNewProduct() {
  fireEvent.click(await screen.findByRole('button', { name: 'Add item' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByRole('searchbox'), { target: { value: 'Jaggery' } });
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Create a new product' }));
  fireEvent.change(within(dialog).getByLabelText('Product name'), { target: { value: 'Jaggery' } });
  fireEvent.change(within(dialog).getByLabelText('Pack size'), { target: { value: '500' } });
  fireEvent.change(within(dialog).getByLabelText('Unit'), { target: { value: 'g' } });
  fireEvent.change(await within(dialog).findByLabelText('Category'), { target: { value: 'c1' } });
  return dialog;
}

describe('B-07 product photo (D-252)', () => {
  it('a new product carries its photo: registered, PUT with only the signed Content-Type, then confirmed', async () => {
    let photoPost: Call | undefined;
    let confirmed = false;
    renderAt('/stock', {
      messages,
      handlers: [
        shops,
        (c) => (c.method === 'POST' && c.path === '/v1/biz/businesses/b1/products' ? json(201, created) : undefined),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/products/p9/photos`) return undefined;
          photoPost = c;
          return json(201, upload);
        },
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/products/p9/photos/ph1/confirm`) return undefined;
          confirmed = true;
          return json(200, { ...upload.photo, status: 'pending', previewUrl: 'https://s3.test/get?sig=y' });
        },
        ...reads([]),
      ],
    });
    const dialog = await openNewProduct();
    const input = within(dialog).getByLabelText('Take or choose a photo');
    expect(input).toHaveAttribute('accept', 'image/*');
    expect(input).toHaveAttribute('capture', 'environment');
    fireEvent.change(input, { target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'jaggery.png', { type: 'image/png' })] } });
    expect(await within(dialog).findByRole('button', { name: 'Remove photo' })).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Create and continue' }));
    await waitFor(() => expect(confirmed).toBe(true));
    expect(photoPost?.body).toEqual({ contentType: 'image/png', sizeBytes: 4 });
    const [url, init] = storage.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://s3.test/put?sig=x');
    expect(init.method).toBe('PUT');
    expect(init.headers).toEqual({ 'Content-Type': 'image/png' });
    expect(await screen.findByText('Photo sent. TodayZ will check it.')).toBeInTheDocument();
    expect(await within(dialog).findByLabelText('Selling price')).toBeInTheDocument();
  });

  it('a failed upload still adds the product and says the photo is missing', async () => {
    storage.mockResolvedValueOnce(new Response(null, { status: 403 }));
    renderAt('/stock', {
      messages,
      handlers: [
        shops,
        (c) => (c.method === 'POST' && c.path === '/v1/biz/businesses/b1/products' ? json(201, created) : undefined),
        (c) => (c.method === 'POST' && c.path === `${BASE}/products/p9/photos` ? json(201, upload) : undefined),
        ...reads([]),
      ],
    });
    const dialog = await openNewProduct();
    fireEvent.change(within(dialog).getByLabelText('Take or choose a photo'), {
      target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'jaggery.png', { type: 'image/png' })] },
    });
    await within(dialog).findByRole('button', { name: 'Remove photo' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create and continue' }));
    expect(await screen.findByText('The product is added, but the photo did not upload.')).toBeInTheDocument();
    expect(await within(dialog).findByLabelText('Selling price')).toBeInTheDocument();
  });

  it('refuses a file that is not a photo, and a product without a photo makes no photo call', async () => {
    const { calls } = renderAt('/stock', {
      messages,
      handlers: [shops, (c) => (c.method === 'POST' && c.path === '/v1/biz/businesses/b1/products' ? json(201, created) : undefined), ...reads([])],
    });
    const dialog = await openNewProduct();
    fireEvent.change(within(dialog).getByLabelText('Take or choose a photo'), { target: { files: [new File(['x'], 'list.pdf', { type: 'application/pdf' })] } });
    expect(await within(dialog).findByText('This file is not a photo we can use. Pick a JPEG, PNG or WebP photo.')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create and continue' }));
    expect(await within(dialog).findByLabelText('Selling price')).toBeInTheDocument();
    expect(calls.some((c) => c.path.includes('/photos'))).toBe(false);
    expect(storage).not.toHaveBeenCalled();
  });

  it('a master-list product shows its picture once branded, and takes a photo after it is added (D-253)', async () => {
    let photoPost: Call | undefined;
    let added = false;
    const match = { id: 'p7', name: 'Daawat Sugar 1 kg', brand: 'Daawat', variant: null, packSize: 1, unit: 'kg', mrpPaise: 5000, categoryId: 'c1', barcodes: [], inStore: false, imageKey: 'types/t1.jpg' };
    renderAt('/stock', {
      messages,
      handlers: [
        shops,
        (c) => (c.method === 'GET' && c.path === '/v1/biz/businesses/b1/products/search' ? json(200, { items: [match], nextCursor: null }) : undefined),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/items`) return undefined;
          added = true;
          return json(201, item('i7', 'Daawat Sugar 1 kg', { productId: 'p7' }));
        },
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/products/p7/photos`) return undefined;
          photoPost = c;
          return json(201, { ...upload, photo: { ...upload.photo, productId: 'p7' } });
        },
        (c) => (c.method === 'POST' && c.path === `${BASE}/products/p7/photos/ph1/confirm` ? json(200, { ...upload.photo, productId: 'p7', status: 'pending' }) : undefined),
        ...reads([]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByRole('searchbox'), { target: { value: 'sugar' } });
    const row = (await within(dialog).findByText('Daawat Sugar 1 kg')).closest('li') as HTMLElement;
    expect(row.querySelector('img')?.getAttribute('src')).toBe('https://img.todayz.localhost/types/t1.jpg');
    fireEvent.click(within(row).getByRole('button', { name: 'Add' }));

    fireEvent.change(await within(dialog).findByLabelText('Selling price'), { target: { value: '48' } });
    fireEvent.change(within(dialog).getByLabelText('Take or choose a photo'), {
      target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'sugar.png', { type: 'image/png' })] },
    });
    await within(dialog).findByRole('button', { name: 'Remove photo' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add to my shop' }));
    await waitFor(() => expect(added).toBe(true));
    await waitFor(() => expect(photoPost?.body).toEqual({ contentType: 'image/png', sizeBytes: 4 }));
    expect(await screen.findByText('Photo sent. TodayZ will check it.')).toBeInTheDocument();
  });

  it('the list shows the approved image, the store photo waiting for review, or the initials; the sheet says why a photo was turned down', async () => {
    const { container } = renderAt('/stock', {
      messages,
      handlers: [
        shops,
        ...reads([
          item('i1', 'Amul Butter', { imageKey: 'products/p-i1/a1.jpg' }),
          item('i2', 'Jaggery', { photo: { id: 'ph2', status: 'pending', previewUrl: 'https://s3.test/get?sig=z', rejectReason: null } }),
          item('i3', 'Toor dal', { photo: { id: 'ph3', status: 'rejected', previewUrl: 'https://s3.test/get?sig=r', rejectReason: 'The pack is blurred' } }),
        ]),
      ],
    });
    await screen.findByText(/Toor dal/);
    const srcs = [...container.querySelectorAll('table img')].map((i) => i.getAttribute('src'));
    expect(srcs).toEqual(['https://img.todayz.localhost/products/p-i1/a1.jpg', 'https://s3.test/get?sig=z']);
    expect(screen.getByText('TD')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /^Toor dal/ }));
    expect(await screen.findByText('Your photo was not accepted: The pack is blurred')).toBeInTheDocument();
  });

  it('the item sheet sends a new photo of a product the store created, and the item is read again', async () => {
    let itemReads = 0;
    let confirmed = false;
    const mine = item('i3', 'Toor dal', { canAddPhoto: true, photo: { id: 'ph3', status: 'rejected', previewUrl: 'https://s3.test/get?sig=r', rejectReason: 'The pack is blurred' } });
    renderAt('/stock', {
      messages,
      handlers: [
        shops,
        (c) => {
          if (c.method === 'GET' && c.path === `${BASE}/items/i3`) itemReads++;
          return undefined;
        },
        (c) => (c.method === 'POST' && c.path === `${BASE}/products/p-i3/photos` ? json(201, { ...upload, photo: { ...upload.photo, productId: 'p-i3' } }) : undefined),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/products/p-i3/photos/ph1/confirm`) return undefined;
          confirmed = true;
          return json(200, { ...upload.photo, productId: 'p-i3', status: 'pending', previewUrl: 'https://s3.test/get?sig=y' });
        },
        ...reads([mine, item('i1', 'Amul Butter', { canAddPhoto: false })]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: /^Toor dal/ }));
    const dialog = await screen.findByRole('dialog');
    const input = await within(dialog).findByLabelText('Take or choose a photo');
    expect(input).toHaveAttribute('capture', 'environment');
    const before = itemReads;
    fireEvent.change(input, { target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'dal.png', { type: 'image/png' })] } });
    await waitFor(() => expect(confirmed).toBe(true));
    const [url, init] = storage.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://s3.test/put?sig=x');
    expect(init.headers).toEqual({ 'Content-Type': 'image/png' });
    await waitFor(() => expect(screen.getByText('Photo sent. TodayZ will check it.')).toBeInTheDocument());
    await waitFor(() => expect(itemReads).toBeGreaterThan(before));
  });

  it('the item sheet has no photo button for a product another store created, and says when the upload fails', async () => {
    storage.mockImplementation(() => Promise.resolve(new Response(null, { status: 403 })));
    renderAt('/stock', {
      messages,
      handlers: [
        shops,
        (c) => (c.method === 'POST' && c.path === `${BASE}/products/p-i3/photos` ? json(201, upload) : undefined),
        ...reads([item('i1', 'Amul Butter', { canAddPhoto: false, imageKey: 'products/p-i1/a1.jpg' }), item('i3', 'Toor dal', { canAddPhoto: true })]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: /^Amul Butter/ }));
    let dialog = await screen.findByRole('dialog');
    await within(dialog).findByText('On hand');
    expect(within(dialog).queryByLabelText('Take or choose a photo')).toBeNull();
    expect(within(dialog).queryByLabelText('Change photo')).toBeNull();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));

    fireEvent.click(await screen.findByRole('button', { name: /^Toor dal/ }));
    dialog = await screen.findByRole('dialog');
    fireEvent.change(await within(dialog).findByLabelText('Take or choose a photo'), {
      target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'dal.png', { type: 'image/png' })] },
    });
    expect(await within(dialog).findByText('The photo did not upload. Try again.')).toBeInTheDocument();
  });
});
