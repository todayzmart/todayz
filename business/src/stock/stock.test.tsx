import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function item(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    storeId: 's1',
    productId: `p-${id}`,
    name: `Item ${id}`,
    productName: `Item ${id}`,
    localName: null,
    brand: null,
    variant: null,
    packSize: 1,
    unit: 'kg',
    categoryId: 'c1',
    barcodes: [],
    pricePaise: 5800,
    mrpPaise: null,
    productMrpPaise: 6000,
    onHand: 12,
    reserved: 2,
    available: 10,
    lowStockAt: null,
    lowStock: false,
    isListed: true,
    stockUpdatedAt: '2026-10-24T08:00:00.000Z',
    stale: false,
    createdAt: '2026-10-01T08:00:00.000Z',
    updatedAt: '2026-10-24T08:00:00.000Z',
    ...over,
  };
}

/** The list, one item read (with an ETag), and the item's batches and history. */
function stockHandlers(items: ReturnType<typeof item>[]): Handler[] {
  return [
    (c) => (c.method === 'GET' && c.path === `${BASE}/items` ? json(200, { items, nextCursor: null }) : undefined),
    (c) => {
      const m = /^\/v1\/biz\/businesses\/b1\/stores\/s1\/items\/([^/]+)$/.exec(c.path);
      const found = m && items.find((i) => i.id === m[1]);
      if (c.method === 'GET' && found) return json(200, found, { ETag: '"v3"' });
      return undefined;
    },
    (c) => (c.method === 'GET' && c.path === `${BASE}/batches` ? json(200, { items: [], nextCursor: null }) : undefined),
    (c) => (c.method === 'GET' && c.path.endsWith('/history') ? json(200, { items: [], nextCursor: null }) : undefined),
    (c) => (c.method === 'GET' && c.path === '/v1/discover/categories' ? json(200, { items: [], nextCursor: null }) : undefined),
  ];
}

describe('B-07 stock list', () => {
  it('lists the items with their stock and, for a cashier, offers counting but not adding or importing', async () => {
    renderAt('/stock', { messages, handlers: [shops('cashier'), ...stockHandlers([item('i1'), item('i2', { available: 0, onHand: 0, reserved: 0 })])] });
    const table = await screen.findByRole('table', { name: 'Stock items' });
    expect(await within(table).findByText('Item i1 · 1 kg')).toBeInTheDocument();
    expect(within(table).getByText('Item i2 · 1 kg')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Count stock' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add item' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Import' })).not.toBeInTheDocument();
  });

  it('gives a manager Add item and Import', async () => {
    renderAt('/stock', { messages, handlers: [shops('manager'), ...stockHandlers([item('i1')])] });
    expect(await screen.findByRole('button', { name: 'Add item' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Import' })).toBeInTheDocument();
  });

  it('shows an error with a retry when the list fails', async () => {
    renderAt('/stock', { messages, handlers: [shops('manager'), (c) => (c.path === `${BASE}/items` ? problem(500, 'internal') : undefined)] });
    expect(await screen.findByText("Couldn't load stock", {}, { timeout: 8000 })).toBeInTheDocument();
  }, 15000);
});

describe('B-07 item sheet and movements', () => {
  it('restock sends the quantity with an Idempotency-Key and shows the held units apart from available', async () => {
    let posted: Call | undefined;
    renderAt('/stock', {
      messages,
      handlers: [
        shops('manager'),
        (c) => {
          if (c.method !== 'POST' || !c.path.endsWith('/items/i1/movements')) return undefined;
          posted = c;
          return json(200, { item: item('i1', { onHand: 17, available: 15 }), movements: [] });
        },
        ...stockHandlers([item('i1')]),
      ],
    });
    fireEvent.click(await screen.findByText('Item i1 · 1 kg'));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Held for orders')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restock' }));
    fireEvent.change(await within(dialog).findByLabelText('Quantity'), { target: { value: '5' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save restock' }));
    await waitFor(() => expect(posted).toBeDefined());
    expect(posted?.body).toMatchObject({ kind: 'restock', quantity: 5 });
    expect(posted?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('a cashier can correct the count but has no price or restock button', async () => {
    renderAt('/stock', { messages, handlers: [shops('cashier'), ...stockHandlers([item('i1')])] });
    fireEvent.click(await screen.findByText('Item i1 · 1 kg'));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('button', { name: 'Correct count' })).toBeEnabled();
    expect(within(dialog).queryByRole('button', { name: 'Change price' })).not.toBeInTheDocument();
    expect(within(dialog).queryByRole('button', { name: 'Restock' })).not.toBeInTheDocument();
  });

  it('a price change reads the item for its ETag, sends If-Match, and may go above the MRP (D-245)', async () => {
    let patch: Call | undefined;
    renderAt('/stock?price=i1', {
      messages,
      handlers: [
        shops('manager'),
        (c) => {
          if (c.method !== 'PATCH' || c.path !== `${BASE}/items/i1`) return undefined;
          patch = c;
          return json(200, item('i1', { pricePaise: 7000 }), { ETag: '"v4"' });
        },
        ...stockHandlers([item('i1')]),
      ],
    });
    const dialog = await screen.findByRole('dialog');
    const price = await within(dialog).findByLabelText('Selling price');
    fireEvent.change(price, { target: { value: '70' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save price' }));
    await waitFor(() => expect(patch).toBeDefined());
    expect(patch?.body).toEqual({ pricePaise: 7000 });
    expect(patch?.headers.get('if-match')).toBe('"v3"');
  });
});

describe('B-07 deep links (D-99)', () => {
  it('restock=<id>&qty= opens the restock form with the quantity filled and applies nothing', async () => {
    const { calls } = renderAt('/stock?restock=i1&qty=12', { messages, handlers: [shops('manager'), ...stockHandlers([item('i1')])] });
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByLabelText('Quantity')).toHaveValue('12');
    expect(calls.some((c) => c.method !== 'GET')).toBe(false);
  });

  it('ignores an unknown item id and leaves the plain list', async () => {
    const { calls } = renderAt('/stock?restock=nope&qty=4', { messages, handlers: [shops('manager'), ...stockHandlers([item('i1')])] });
    await waitFor(() => expect(calls.some((c) => c.path === `${BASE}/items/nope`)).toBe(true));
    await waitFor(() => expect(screen.getByText('Item i1 · 1 kg')).toBeInTheDocument());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('count=a,b opens count mode with those items only', async () => {
    renderAt('/stock?count=i1,i2', { messages, handlers: [shops('cashier'), ...stockHandlers([item('i1'), item('i2'), item('i3')])] });
    const table = await screen.findByRole('table', { name: 'Stock items' });
    expect(await within(table).findByText('Item i1 · 1 kg')).toBeInTheDocument();
    expect(within(table).getByText('Item i2 · 1 kg')).toBeInTheDocument();
    expect(within(table).queryByText('Item i3 · 1 kg')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Done counting' })).toBeInTheDocument();
  });

  it('a link to an action the role cannot do says to ask a manager', async () => {
    renderAt('/stock?price=i1', { messages, handlers: [shops('cashier'), ...stockHandlers([item('i1')])] });
    const dialog = await screen.findByRole('dialog');
    expect(await within(dialog).findByText('Ask a manager to do this.')).toBeInTheDocument();
    expect(within(dialog).queryByLabelText('Selling price')).not.toBeInTheDocument();
  });
});

const CATEGORY = { id: 'c1', slug: 'grocery', name: 'Grocery', names: {}, tint: 'mint', icon: 'shopping-basket', children: [] };

describe('B-07 import and create product (adapter endpoints)', () => {
  it('shows every row checked, with its problems, before anything is saved; apply sends an Idempotency-Key', async () => {
    let applied: Call | undefined;
    const preview = {
      id: 'imp1',
      fileName: 'items.csv',
      rows: [
        { row: 2, status: 'ok', name: 'Toor dal 1 kg', barcode: null, pricePaise: 12900, quantity: 4, errors: [] },
        { row: 3, status: 'error', name: 'Sugar', barcode: null, pricePaise: 9000, quantity: 2, errors: [{ field: 'pricePaise', code: 'price_above_mrp' }] },
      ],
      counts: { ok: 1, error: 1 },
    };
    const { calls } = renderAt('/stock', {
      messages,
      handlers: [
        shops('manager'),
        (c) => (c.method === 'POST' && c.path === `${BASE}/item-imports` ? json(200, preview) : undefined),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/item-imports/imp1/apply`) return undefined;
          applied = c;
          return json(200, { applied: 1, skipped: 1 });
        },
        ...stockHandlers([item('i1')]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Import' }));
    const dialog = await screen.findByRole('dialog');
    const file = new File(['name,price\nToor dal,129'], 'items.csv', { type: 'text/csv' });
    fireEvent.change(within(dialog).getByLabelText('Items file'), { target: { files: [file] } });
    expect(await within(dialog).findByText(/Price: is above the MRP/)).toBeInTheDocument();
    expect(within(dialog).getByText('1 row ready')).toBeInTheDocument();
    expect(applied).toBeUndefined();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Import 1 item' }));
    expect(await within(dialog).findByText(/1 item imported/)).toBeInTheDocument();
    expect(applied?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(calls.filter((c) => c.path === `${BASE}/item-imports`)).toHaveLength(1);
  });

  it('creates a missing product, then asks for the price and opening stock; the price may be above the MRP (D-245)', async () => {
    let created: Call | undefined;
    let added: Call | undefined;
    renderAt('/stock', {
      messages,
      handlers: [
        shops('manager'),
        (c) => (c.method === 'GET' && c.path === '/v1/discover/categories' ? json(200, { items: [CATEGORY], nextCursor: null }) : undefined),
        (c) => (c.method === 'GET' && c.path === '/v1/biz/businesses/b1/products/search' ? json(200, { items: [], nextCursor: null }) : undefined),
        (c) => {
          if (c.method !== 'POST' || c.path !== '/v1/biz/businesses/b1/products') return undefined;
          created = c;
          return json(201, { id: 'p9', name: 'Jaggery', brand: null, variant: null, packSize: 500, unit: 'g', mrpPaise: 6000, categoryId: 'c1', barcodes: [], status: 'pending_review' });
        },
        (c) => {
          if (c.method !== 'POST' || c.path !== `${BASE}/items`) return undefined;
          added = c;
          return json(201, item('i9', { pricePaise: 7500 }));
        },
        ...stockHandlers([item('i1')]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByRole('searchbox'), { target: { value: 'Jaggery' } });
    fireEvent.click(await within(dialog).findByRole('button', { name: 'Create a new product' }));
    fireEvent.change(within(dialog).getByLabelText('Product name'), { target: { value: 'Jaggery' } });
    fireEvent.change(within(dialog).getByLabelText('Pack size'), { target: { value: '500' } });
    fireEvent.change(within(dialog).getByLabelText('Unit'), { target: { value: 'g' } });
    fireEvent.change(within(dialog).getByLabelText('Category'), { target: { value: 'c1' } });
    fireEvent.change(within(dialog).getByLabelText('MRP'), { target: { value: '60' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create and continue' }));
    await waitFor(() => expect(created).toBeDefined());
    expect(created?.body).toMatchObject({ storeId: 's1', name: 'Jaggery', packSize: 500, unit: 'g', categoryId: 'c1', mrpPaise: 6000 });
    expect(created?.headers.get('idempotency-key')).toBeTruthy();
    fireEvent.change(await within(dialog).findByLabelText('Selling price'), { target: { value: '75' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add to my shop' }));
    await waitFor(() => expect(added).toBeDefined());
    expect(added?.body).toMatchObject({ productId: 'p9', pricePaise: 7500 });
  });
});
