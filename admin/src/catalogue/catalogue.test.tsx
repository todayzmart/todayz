import type { AdminCategory } from '@todayz/api-client/gen/model/adminCategory';
import type { AdminPendingProduct } from '@todayz/api-client/gen/model/adminPendingProduct';
import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { AdminSynonym } from '@todayz/api-client/gen/model/adminSynonym';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { parseBarcodes, parseMrp, parsePack, rupees } from './form';
import { validateCatalogueSearch } from './search';

const TOOR = '01JPR0DXCT00000000000000T1';
const PENDING = '01JPR0DXCT00000000000000P1';
const STAPLES = '01JCATEG0RY0000000000000S1';
const GROCERY = '01JCATEG0RY0000000000000G1';
const SYN = '01JSYN0NYM00000000000000Y1';

function product(over: Partial<AdminProduct> = {}): AdminProduct {
  return {
    id: TOOR,
    name: 'Toor Dal',
    brandName: 'Tata Sampann',
    categoryId: STAPLES,
    categoryName: 'Staples',
    variant: 'Unpolished',
    packSize: 1,
    unit: 'kg',
    mrpPaise: 18900,
    barcodes: ['8901725181234'],
    status: 'active',
    mergedInto: null,
    storeCount: 14,
    createdAt: '2026-10-01T09:00:00Z',
    updatedAt: '2026-10-20T09:00:00Z',
    ...over,
  };
}

function pending(): AdminPendingProduct {
  return {
    product: product({ id: PENDING, name: 'Toor dal unpolished', brandName: null, variant: null, mrpPaise: null, barcodes: [], status: 'pending_review', storeCount: 0 }),
    createdByStoreId: STORE_ID,
    createdByStoreName: 'Annapurna Kirana',
    suggestions: [{ product: product(), similarity: 0.62 }],
  };
}

function category(over: Partial<AdminCategory> = {}): AdminCategory {
  return { id: STAPLES, parentId: null, slug: 'staples', names: { en: 'Staples', hi: 'राशन' }, tint: 'amber', icon: 'wheat', sort: 10, productCount: 240, updatedAt: NOW, ...over };
}

function synonym(over: Partial<AdminSynonym> = {}): AdminSynonym {
  return { id: SYN, term: 'curd', synonym: 'dahi', lang: 'hi', productId: null, productName: null, createdAt: NOW, ...over };
}

function api(opts: { assist?: boolean } = {}): Handler {
  let current = product();
  let queue = [pending()];
  let synonyms = [synonym()];
  const categories = [category(), category({ id: GROCERY, slug: 'grocery', names: { en: 'Grocery' }, sort: 5 })];
  return (c) => {
    if (c.path === '/v1/admin/products' && c.method === 'GET') return json(200, { items: [current], nextCursor: null, serverNow: NOW });
    if (c.path === '/v1/admin/products' && c.method === 'POST') {
      const b = c.body as { barcodes?: string[] };
      if (b.barcodes?.includes('8901725181234')) return problem(409, 'conflict');
      return json(201, { ...product(), ...(c.body as object), id: '01JPR0DXCT00000000000000N1', storeCount: 0 });
    }
    if (c.path === `/v1/admin/products/${TOOR}` && c.method === 'PATCH') {
      const { reason: _r, ...rest } = c.body as Record<string, unknown>;
      current = { ...current, ...rest } as AdminProduct;
      return json(200, current);
    }
    if (c.path === '/v1/admin/pending-products' && c.method === 'GET') {
      return json(200, { items: queue, assistEnabled: opts.assist ?? false, nextCursor: null, serverNow: NOW });
    }
    if (c.path.startsWith(`/v1/admin/pending-products/${PENDING}/`) && c.method === 'POST') {
      queue = [];
      const merged = c.path.endsWith('/merge');
      return json(200, { ...pending().product, status: merged ? 'merged' : c.path.endsWith('/reject') ? 'retired' : 'active', mergedInto: merged ? TOOR : null });
    }
    if (c.path === '/v1/admin/categories' && c.method === 'GET') return json(200, { items: categories });
    if (c.path === '/v1/admin/categories' && c.method === 'POST') {
      return json(201, { ...category(), ...(c.body as object), id: '01JCATEG0RY0000000000000N1', productCount: 0 });
    }
    if (c.path === `/v1/admin/categories/${STAPLES}` && c.method === 'PATCH') return json(200, category());
    if (c.path === '/v1/admin/synonyms' && c.method === 'GET') return json(200, { items: synonyms, nextCursor: null });
    if (c.path === '/v1/admin/synonyms' && c.method === 'POST') {
      const b = c.body as { term: string; synonym: string; lang: 'en' };
      if (b.term === 'curd' && b.synonym === 'dahi') return problem(409, 'conflict');
      const row = synonym({ id: '01JSYN0NYM00000000000000N1', ...b });
      synonyms = [...synonyms, row];
      return json(201, row);
    }
    if (c.path === `/v1/admin/synonyms/${SYN}` && c.method === 'DELETE') {
      synonyms = synonyms.filter((s) => s.id !== SYN);
      return new Response(null, { status: 204 });
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('catalogue search and forms', () => {
  it('keeps only well-formed parameters', () => {
    expect(validateCatalogueSearch({ tab: 'review', q: ' amul ', status: 'retired', categoryId: STAPLES.toLowerCase(), open: TOOR, x: 1 })).toEqual({
      tab: 'review',
      q: 'amul',
      status: 'retired',
      categoryId: STAPLES,
      open: TOOR,
    });
    expect(validateCatalogueSearch({ tab: 'products', status: 'deleted', categoryId: 'staples', q: 'x'.repeat(81) })).toEqual({});
  });

  it('parses pack sizes, rupees and barcodes the way the contract bounds them', () => {
    expect(parsePack('0.5')).toEqual({ value: 0.5 });
    expect(parsePack('0')).toEqual({ error: 'pack' });
    expect(parseMrp('189.5')).toEqual({ value: 18950 });
    expect(parseMrp('1,240')).toEqual({ value: 124000 });
    expect(parseMrp('')).toEqual({ value: null });
    expect(parseMrp('0')).toEqual({ error: 'mrp' });
    expect(parseMrp('1.234')).toEqual({ error: 'mrp' });
    expect(rupees(18950)).toBe('189.50');
    expect(rupees(18900)).toBe('189');
    expect(parseBarcodes('89017251 8901725181234, 89017251')).toEqual({ value: ['89017251', '8901725181234'] });
    expect(parseBarcodes('1234')).toEqual({ error: 'barcode' });
    expect(parseBarcodes(Array.from({ length: 11 }, (_, i) => String(10000000 + i)).join(' '))).toEqual({ error: 'barcodes' });
  });
});

describe('A-08 catalogue', () => {
  it('lets the catalogue desk read products without the admin edits, filters in the URL', async () => {
    const { calls, router } = renderAt('/catalogue', { roles: ['catalogue'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Products' });
    expect(await within(table).findByText('Tata Sampann')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add product' })).not.toBeInTheDocument();

    const form = screen.getByRole('search', { name: 'Product filters' });
    fireEvent.change(within(form).getByLabelText('Search'), { target: { value: ' toor ' } });
    fireEvent.change(within(form).getByLabelText('Status'), { target: { value: 'active' } });
    fireEvent.change(await within(form).findByLabelText('Category'), { target: { value: STAPLES } });
    fireEvent.click(within(form).getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ q: 'toor', status: 'active', categoryId: STAPLES }));
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/products')).toHaveLength(2));
    const q = new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/products')[1]?.search);
    expect([q.get('q'), q.get('status'), q.get('categoryId')]).toEqual(['toor', 'active', STAPLES]);

    fireEvent.click(await screen.findByRole('button', { name: 'Toor Dal · Unpolished' }));
    expect(await screen.findByRole('heading', { name: 'Toor Dal · Unpolished' })).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument();
  });

  it('adds a product, and names a taken barcode', async () => {
    const { calls, router } = renderAt('/catalogue', { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Add product' }));
    const dialog = await screen.findByRole('dialog', { name: 'Add a product' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add product' }));
    expect(await within(dialog).findByText('Enter a name, up to 120 characters.')).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/products' && c.method === 'POST')).toBe(false);

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: ' Moong Dal ' } });
    fireEvent.change(await within(dialog).findByLabelText('Category'), { target: { value: STAPLES } });
    fireEvent.change(within(dialog).getByLabelText('Pack size'), { target: { value: '500' } });
    fireEvent.change(within(dialog).getByLabelText('Unit'), { target: { value: 'g' } });
    fireEvent.change(within(dialog).getByLabelText('MRP'), { target: { value: '95.50' } });
    fireEvent.change(within(dialog).getByLabelText('Barcodes'), { target: { value: '8901725181234' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add product' }));
    expect(await within(dialog).findByText('Another product already has one of these barcodes.')).toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText('Barcodes'), { target: { value: '8901725199999' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add product' }));
    await waitFor(() => expect(screen.getByText('Product added')).toBeInTheDocument());
    expect(calls.filter((c) => c.path === '/v1/admin/products' && c.method === 'POST')[1]?.body).toEqual({
      name: 'Moong Dal',
      categoryId: STAPLES,
      packSize: 500,
      unit: 'g',
      mrpPaise: 9550,
      barcodes: ['8901725199999'],
    });
    await waitFor(() => expect(router.state.location.search).toEqual({ open: '01JPR0DXCT00000000000000N1' }));
  });

  it('edits only what changed, clears the variant to null, and needs a reason', async () => {
    const { calls } = renderAt(`/catalogue?open=${TOOR}`, { roles: ['super_admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const dialog = await screen.findByRole('dialog', { name: 'Edit Toor Dal · Unpolished' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'checked the shelf' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    expect(await within(dialog).findByText('Nothing has changed.')).toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText('Variant'), { target: { value: ' ' } });
    fireEvent.change(within(dialog).getByLabelText('MRP'), { target: { value: '199' } });
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Retired' }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.getByText('Product saved')).toBeInTheDocument());
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ reason: 'checked the shelf', variant: null, mrpPaise: 19900, status: 'retired' });
  });

  it('works the review queue: suggestions, merge into one, the assistant slot only when on', async () => {
    const { calls, container } = renderAt('/catalogue?tab=review', { roles: ['catalogue'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Products to review' });
    fireEvent.click(await within(table).findByRole('button', { name: 'Toor dal unpolished' }));
    expect(await screen.findByText('1 kg · 62% alike · 14 stores')).toBeInTheDocument();
    expect(container.querySelector('[data-slot="catalogue-assist"]')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Merge into this' }));
    const dialog = await screen.findByRole('dialog', { name: 'Merge Toor dal unpolished into Toor Dal · Unpolished?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Merge' }));
    await waitFor(() => expect(screen.getByText('Merged')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/merge'))?.body).toEqual({ intoProductId: TOOR });
    expect(await screen.findByText('Nothing to review. New products from stores show up here.')).toBeInTheDocument();
    expect(screen.getByText('This product is no longer waiting for review. Its history is below.')).toBeInTheDocument();
  });

  it('mounts the assistant slot when ai_catalogue_enabled is on, and rejects only with a reason', async () => {
    const { calls, container } = renderAt(`/catalogue?tab=review&open=${PENDING}`, { roles: ['admin'], handlers: [api({ assist: true })] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reject' }));
    expect(container.querySelector(`[data-slot="catalogue-assist"][data-product="${PENDING}"]`)).not.toBeNull();
    const dialog = await screen.findByRole('dialog', { name: 'Reject Toor dal unpolished?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    expect(await within(dialog).findByText("Write a reason, up to 500 characters.")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'a service, not a product' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    await waitFor(() => expect(screen.getByText('Rejected')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/reject'))?.body).toEqual({ reason: 'a service, not a product' });
  });

  it('approves with an empty body when no note is given', async () => {
    const { calls } = renderAt(`/catalogue?tab=review&open=${PENDING}`, { roles: ['catalogue'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }));
    const dialog = await screen.findByRole('dialog', { name: 'Approve Toor dal unpolished?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(screen.getByText('Approved')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/approve'))?.body).toEqual({});
  });

  it('adds a category under a top-level one and edits names with a reason', async () => {
    const { calls } = renderAt('/catalogue?tab=categories', { roles: ['admin'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Categories' });
    expect(await within(table).findByText('staples')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Add category' }));
    let dialog = await screen.findByRole('dialog', { name: 'Add a category' });
    fireEvent.change(within(dialog).getByLabelText('Slug'), { target: { value: 'Festive' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add category' }));
    expect(await within(dialog).findByText('Use lower-case letters, digits and single hyphens.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Slug'), { target: { value: 'festive' } });
    fireEvent.change(within(dialog).getByLabelText('Inside'), { target: { value: GROCERY } });
    fireEvent.change(within(dialog).getByLabelText('Name in English'), { target: { value: ' Festive ' } });
    fireEvent.change(within(dialog).getByLabelText('Name in Hindi'), { target: { value: 'त्योहार' } });
    fireEvent.change(within(dialog).getByLabelText('Icon'), { target: { value: 'sparkles' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add category' }));
    await waitFor(() => expect(screen.getByText('Category added')).toBeInTheDocument());
    expect(calls.find((c) => c.path === '/v1/admin/categories' && c.method === 'POST')?.body).toEqual({
      slug: 'festive',
      names: { en: 'Festive', hi: 'त्योहार' },
      tint: 'green',
      icon: 'sparkles',
      parentId: GROCERY,
    });

    fireEvent.click(await within(table).findByRole('button', { name: 'Staples' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    dialog = await screen.findByRole('dialog', { name: 'Edit Staples' });
    fireEvent.change(within(dialog).getByLabelText('Name in Tamil'), { target: { value: 'மளிகை' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Tamil name reviewed' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.getByText('Category saved')).toBeInTheDocument());
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ reason: 'Tamil name reviewed', names: { en: 'Staples', hi: 'राशन', ta: 'மளிகை' } });
  });

  it('adds and removes search words; a taken pair says so', async () => {
    const { calls } = renderAt('/catalogue?tab=synonyms', { roles: ['admin'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Search words' });
    expect(await within(table).findByText('dahi')).toBeInTheDocument();
    const add = screen.getByRole('region', { name: 'Add a search word' });
    fireEvent.change(within(add).getByLabelText('Word'), { target: { value: ' Curd ' } });
    fireEvent.change(within(add).getByLabelText('Also matches'), { target: { value: 'DAHI' } });
    fireEvent.click(within(add).getByRole('button', { name: 'Add' }));
    expect(await within(add).findByText('This pair is already there.')).toBeInTheDocument();
    fireEvent.change(within(add).getByLabelText('Word'), { target: { value: 'akash kandil' } });
    fireEvent.change(within(add).getByLabelText('Also matches'), { target: { value: 'Diwali Lantern' } });
    fireEvent.click(within(add).getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(screen.getByText('Search word added')).toBeInTheDocument());
    expect(calls.filter((c) => c.path === '/v1/admin/synonyms' && c.method === 'POST')[1]?.body).toEqual({
      term: 'akash kandil',
      synonym: 'diwali lantern',
      lang: 'en',
    });

    fireEvent.click(await within(table).findByRole('button', { name: 'Remove curd → dahi' }));
    const dialog = await screen.findByRole('dialog', { name: 'Remove this search word?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(screen.getByText('Search word removed')).toBeInTheDocument());
    expect(calls.some((c) => c.method === 'DELETE' && c.path === `/v1/admin/synonyms/${SYN}`)).toBe(true);
  });

  it('shows the catalogue desk the words without add or remove', async () => {
    renderAt('/catalogue?tab=synonyms', { roles: ['catalogue'], handlers: [api()] });
    expect(await screen.findByText('dahi')).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Add a search word' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Remove/ })).not.toBeInTheDocument();
  });
});
