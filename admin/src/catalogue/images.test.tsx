import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { AdminProductImage } from '@todayz/api-client/gen/model/adminProductImage';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateCatalogueSearch } from './search';

const TOOR = '01JPR0DXCT00000000000000T1';
const BUTTER = '01JPR0DXCT00000000000000B1';
const STAPLES = '01JCATEG0RY0000000000000S1';
const OFF_IMG = '01JPH0T0000000000000000PA1';
const SHOP_IMG = '01JPH0T0000000000000000PB1';
const NEW_IMG = '01JPH0T0000000000000000PN1';

afterEach(() => vi.unstubAllGlobals());

function product(over: Partial<AdminProduct> = {}): AdminProduct {
  return {
    id: TOOR,
    name: 'Toor Dal',
    brandName: 'Tata Sampann',
    categoryId: STAPLES,
    categoryName: 'Staples',
    variant: null,
    packSize: 1,
    unit: 'kg',
    mrpPaise: 18900,
    barcodes: [],
    status: 'active',
    mergedInto: null,
    storeCount: 14,
    imageKey: null,
    createdAt: '2026-10-01T09:00:00Z',
    updatedAt: '2026-10-20T09:00:00Z',
    ...over,
  };
}

function image(over: Partial<AdminProductImage> = {}): AdminProductImage {
  return {
    id: OFF_IMG,
    productId: BUTTER,
    productName: 'Amul Butter',
    brandName: 'Amul',
    categoryName: 'Dairy',
    productStatus: 'active',
    productImageKey: null,
    source: 'off',
    status: 'pending',
    contentType: 'image/jpeg',
    sizeBytes: 48_000,
    previewUrl: 'https://s3.test/get?sig=off',
    imageKey: null,
    sourceRef: '8901262010016',
    sourceUrl: 'https://images.openfoodfacts.org/images/products/890/126/201/0016/front_en.jpg',
    pageUrl: 'https://world.openfoodfacts.org/product/8901262010016',
    licence: 'CC BY-SA 3.0',
    attribution: 'Open Food Facts contributors',
    match: 'barcode',
    storeId: null,
    storeName: null,
    rejectReason: null,
    decidedAt: null,
    createdAt: '2026-10-25T05:00:00Z',
    ...over,
  };
}

const shopPhoto = () =>
  image({
    id: SHOP_IMG,
    productId: TOOR,
    productName: 'Toor Dal',
    brandName: 'Tata Sampann',
    categoryName: 'Staples',
    source: 'retailer',
    previewUrl: 'https://s3.test/get?sig=shop',
    sourceRef: null,
    sourceUrl: null,
    pageUrl: null,
    licence: null,
    attribution: null,
    match: null,
    storeId: STORE_ID,
    storeName: 'Annapurna Kirana',
  });

function api(opts: { bulkFails?: string } = {}): Handler {
  let queue = [image(), shopPhoto()];
  let toor = product();
  return (c) => {
    if (c.path === '/v1/admin/product-images' && c.method === 'GET') {
      const source = new URLSearchParams(c.search).get('source');
      return json(200, { items: queue.filter((i) => !source || i.source === source), nextCursor: null, serverNow: NOW });
    }
    const one = /^\/v1\/admin\/product-images\/(\w+)\/(approve|reject)$/.exec(c.path);
    if (one && c.method === 'POST') {
      const found = queue.find((i) => i.id === one[1]);
      if (!found) return problem(409, 'invalid_transition');
      queue = queue.filter((i) => i.id !== one[1]);
      return json(200, { ...found, status: one[2] === 'approve' ? 'approved' : 'rejected' });
    }
    if (c.path === '/v1/admin/product-image-approvals' && c.method === 'POST') {
      const ids = (c.body as { imageIds: string[] }).imageIds;
      const approved = queue.filter((i) => ids.includes(i.id) && i.id !== opts.bulkFails).map((i) => ({ ...i, status: 'approved' }));
      const failed = ids.filter((id) => id === opts.bulkFails).map((imageId) => ({ imageId, code: 'invalid_transition', detail: 'The product was merged.' }));
      queue = queue.filter((i) => !approved.some((a) => a.id === i.id));
      return json(200, { approved, failed });
    }
    if (c.path === '/v1/admin/products' && c.method === 'GET') {
      return json(200, { items: [toor, product({ id: BUTTER, name: 'Amul Butter', brandName: 'Amul', imageKey: 'products/b1/a1.jpg' })], nextCursor: null, serverNow: NOW });
    }
    if (c.path === `/v1/admin/products/${TOOR}/images` && c.method === 'POST') {
      return json(201, {
        image: image({ id: NEW_IMG, productId: TOOR, productName: 'Toor Dal', source: 'admin', status: 'uploading', previewUrl: null }),
        upload: { url: 'https://s3.test/put?sig=a', method: 'PUT', headers: { 'Content-Type': 'image/png', 'Content-Length': '4' }, expiresAt: NOW },
      });
    }
    if (c.path === `/v1/admin/product-images/${NEW_IMG}/confirm` && c.method === 'POST') {
      toor = { ...toor, imageKey: `products/${TOOR}/${NEW_IMG}.png` };
      return json(200, image({ id: NEW_IMG, productId: TOOR, source: 'admin', status: 'approved', imageKey: toor.imageKey }));
    }
    if (c.path === '/v1/admin/categories' && c.method === 'GET') return json(200, { items: [] });
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('A-08 product images (D-252)', () => {
  it('keeps the images tab and its source filter in the URL', () => {
    expect(validateCatalogueSearch({ tab: 'images', source: 'off', open: OFF_IMG })).toEqual({ tab: 'images', source: 'off', open: OFF_IMG });
    expect(validateCatalogueSearch({ tab: 'images', source: 'zepto' })).toEqual({ tab: 'images' });
  });

  it('shows an imported image with its source, match, licence and credit, and approves it', async () => {
    const { calls } = renderAt(`/catalogue?tab=images&open=${OFF_IMG}`, { roles: ['catalogue'], handlers: [api()] });
    await screen.findByRole('button', { name: 'Toor Dal' });
    const table = screen.getByRole('table', { name: 'Images waiting for review' });
    expect(within(table).getByText('Open Food Facts')).toBeInTheDocument();
    expect(within(table).getByText('Shop photo')).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/admin/product-images')?.search).toContain('status=pending');

    const card = await screen.findByRole('region', { name: 'Amul Butter' });
    expect(within(card).getByRole('img', { name: 'Amul Butter, the image waiting for review' })).toHaveAttribute('src', 'https://s3.test/get?sig=off');
    expect(within(card).getByText('No image yet')).toBeInTheDocument();
    expect(within(card).getByText('Same barcode')).toBeInTheDocument();
    expect(within(card).getByText('8901262010016')).toBeInTheDocument();
    expect(within(card).getByText('CC BY-SA 3.0')).toBeInTheDocument();
    expect(within(card).getByRole('link', { name: 'Source page' })).toHaveAttribute('href', 'https://world.openfoodfacts.org/product/8901262010016');

    fireEvent.click(within(card).getByRole('button', { name: 'Approve image' }));
    const dialog = await screen.findByRole('dialog', { name: 'Approve the image for Amul Butter?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve image' }));
    await waitFor(() => expect(screen.getByText('Image approved.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/product-images/${OFF_IMG}/approve`)?.body).toEqual({});
  });

  it("rejects a shop's photo only with a reason the shop will see", async () => {
    const { calls } = renderAt(`/catalogue?tab=images&open=${SHOP_IMG}`, { roles: ['catalogue'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'Toor Dal' });
    expect(within(card).getByText('Annapurna Kirana')).toBeInTheDocument();
    fireEvent.click(within(card).getByRole('button', { name: 'Reject image' }));
    const dialog = await screen.findByRole('dialog', { name: 'Reject the image for Toor Dal?' });
    expect(within(dialog).getByText(/The shop sees your reason/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'The pack is blurred' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject image' }));
    await waitFor(() => expect(screen.getByText('Image rejected.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/product-images/${SHOP_IMG}/reject`)?.body).toEqual({ reason: 'The pack is blurred' });
  });

  it('approves the chosen images in bulk and lists the ones that could not be approved', async () => {
    const { calls } = renderAt('/catalogue?tab=images', { roles: ['catalogue'], handlers: [api({ bulkFails: SHOP_IMG })] });
    await screen.findByRole('button', { name: 'Toor Dal' });
    expect(screen.getByRole('button', { name: 'Approve chosen (0)' })).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Choose every image shown' }));
    fireEvent.click(screen.getByRole('button', { name: 'Approve chosen (2)' }));
    const dialog = await screen.findByRole('dialog', { name: 'Approve 2 images?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve chosen (2)' }));
    await waitFor(() => expect(screen.getByText('1 image approved.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === '/v1/admin/product-image-approvals')?.body).toEqual({ imageIds: [OFF_IMG, SHOP_IMG] });
    expect(screen.getByRole('alert')).toHaveTextContent('Not approved: Toor Dal. The product was merged.');
  });

  it('filters the queue by source', async () => {
    const { calls } = renderAt('/catalogue?tab=images', { roles: ['catalogue'], handlers: [api()] });
    await screen.findByRole('button', { name: 'Toor Dal' });
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'retailer' } });
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/product-images' && c.search.includes('source=retailer'))).toBe(true));
    const table = screen.getByRole('table', { name: 'Images waiting for review' });
    await waitFor(() => expect(within(table).queryByText('Open Food Facts')).not.toBeInTheDocument());
  });

  it('shows product thumbnails; the catalogue desk cannot upload, an admin uploads and the image goes live', async () => {
    const put = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal('fetch', put);
    const desk = renderAt(`/catalogue?open=${TOOR}`, { roles: ['catalogue'], handlers: [api()] });
    await screen.findByRole('button', { name: 'Amul Butter' });
    const table = screen.getByRole('table', { name: 'Products' });
    expect(desk.container.querySelector('table img')?.getAttribute('src')).toBe('https://img.todayz.localhost/products/b1/a1.jpg');
    expect(within(table).getByText('TD')).toBeInTheDocument();
    await screen.findByText('No image yet. Customers see the initials.');
    expect(screen.queryByRole('button', { name: 'Upload image' })).not.toBeInTheDocument();
    desk.unmount();

    const { calls, getByTestId } = renderAt(`/catalogue?open=${TOOR}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Upload image' }));
    const input = getByTestId('catalogue-image-input');
    fireEvent.change(input, { target: { files: [new File(['x'], 'list.pdf', { type: 'application/pdf' })] } });
    await waitFor(() => expect(screen.getByText('Pick a JPEG, PNG or WebP image.')).toBeInTheDocument());
    fireEvent.change(input, { target: { files: [new File([new Uint8Array([1, 2, 3, 4])], 'toor.png', { type: 'image/png' })] } });
    await waitFor(() => expect(screen.getByText('Image published.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/products/${TOOR}/images`)?.body).toEqual({ contentType: 'image/png', sizeBytes: 4 });
    expect(put).toHaveBeenCalledWith('https://s3.test/put?sig=a', expect.objectContaining({ method: 'PUT', headers: { 'Content-Type': 'image/png' } }));
    expect(calls.find((c) => c.path === `/v1/admin/product-images/${NEW_IMG}/confirm`)?.body).toEqual({});
    expect(await screen.findByRole('button', { name: 'Replace image' })).toBeInTheDocument();
  });
});
