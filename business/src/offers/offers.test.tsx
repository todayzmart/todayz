import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { failThenHold, holding } from '../test/held';
import { sampleLines } from './preview';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function item(id: string, pricePaise = 5800) {
  return {
    id, storeId: 's1', productId: `p-${id}`, name: `Item ${id}`, productName: `Item ${id}`, localName: null, brand: null, variant: null,
    packSize: 1, unit: 'kg', categoryId: 'c1', barcodes: [], pricePaise, mrpPaise: null, productMrpPaise: 6000, onHand: 12, reserved: 0,
    available: 12, lowStockAt: null, lowStock: false, isListed: true, stockUpdatedAt: '2026-10-24T08:00:00.000Z', stale: false,
    createdAt: '2026-10-01T08:00:00.000Z', updatedAt: '2026-10-24T08:00:00.000Z',
  };
}

function offer(id: string, over: Record<string, unknown> = {}) {
  return {
    id, storeId: 's1', title: `Offer ${id}`, kind: 'percent', value: 20, buyQty: null, getQty: null, minBillPaise: null, scope: 'items',
    storeItemIds: ['i1'], categoryId: null, channels: 'both', perCustomerLimit: null, maxRedemptions: 50, redemptions: 7,
    startsAt: '2026-10-20T08:00:00.000Z', endsAt: null, status: 'live', source: 'manual', batchId: null, version: 3,
    createdAt: '2026-10-20T08:00:00.000Z', updatedAt: '2026-10-20T08:00:00.000Z', ...over,
  };
}

function banner(id: string, over: Record<string, unknown> = {}) {
  return {
    id, storeId: 's1', headline: `Banner ${id}`, imageKey: null, template: { tint: 'green' }, targetType: 'store', targetId: null,
    targetItemIds: [], startsAt: '2026-10-20T08:00:00.000Z', endsAt: null, status: 'live', moderation: 'auto_ok', removeReason: null,
    impressions: 120, clicks: 9, attributedOrders: 2, version: 1, createdAt: '2026-10-20T08:00:00.000Z', updatedAt: '2026-10-20T08:00:00.000Z', ...over,
  };
}

const offers = (items: unknown[]): Handler => (c) => (c.method === 'GET' && c.path === `${BASE}/offers` ? json(200, { items, nextCursor: null }) : undefined);
const banners = (items: unknown[]): Handler => (c) => (c.method === 'GET' && c.path === `${BASE}/banners` ? json(200, { items, nextCursor: null }) : undefined);
const items = (list: unknown[]): Handler => (c) => (c.method === 'GET' && c.path === `${BASE}/items` ? json(200, { items: list, nextCursor: null }) : undefined);
const preview: Handler = (c) =>
  c.path === `${BASE}/offer-previews`
    ? json(200, {
        title: '20% off', lines: [{ storeItemId: 'i1', quantity: 1, unitPricePaise: 5800, grossPaise: 5800, discountPaise: 1160, totalPaise: 4640, freeUnits: 0, byThisOffer: true }],
        grossPaise: 5800, itemDiscountPaise: 1160, billDiscountPaise: 0, totalPaise: 4640, totalWithoutPaise: 5800, applies: true,
      })
    : undefined;

describe('B-08 offers', () => {
  it('lists offers with status and redemptions', async () => {
    renderAt('/offers', { messages, handlers: [shops('owner'), offers([offer('o1'), offer('o2', { status: 'paused', title: 'Weekend', redemptions: 0, maxRedemptions: null })])] });
    expect(await screen.findByText('Offer o1')).toBeInTheDocument();
    expect(screen.getByText(/7 of 50 used/)).toBeInTheDocument();
    expect(screen.getAllByText('Live').length).toBeGreaterThan(1);
    expect(screen.getAllByText('Paused').length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Resume' })).toBeInTheDocument();
  });

  it('sends a cashier to ask a manager and loads nothing', async () => {
    let called = false;
    renderAt('/offers', { messages, handlers: [shops('cashier'), (c) => ((called = called || c.path.endsWith('/offers')), undefined)] });
    expect(await screen.findByText(/Ask a manager/)).toBeInTheDocument();
    expect(called).toBe(false);
  });

  it('pauses with the offer version in If-Match, and ends only after a second tap', async () => {
    const patches: Call[] = [];
    renderAt('/offers', {
      messages,
      handlers: [
        shops('manager'),
        (c) => {
          if (c.method === 'PATCH' && c.path === `${BASE}/offers/o1`) {
            patches.push(c);
            return json(200, offer('o1', { status: (c.body as { status: string }).status === 'ended' ? 'ended' : 'paused', version: 4 }));
          }
          return undefined;
        },
        offers([offer('o1')]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    await waitFor(() => expect(patches).toHaveLength(1));
    expect(patches[0]?.headers.get('if-match')).toBe('"v3"');
    expect(patches[0]?.body).toEqual({ status: 'paused' });

    fireEvent.click(await screen.findByRole('button', { name: 'End' }));
    expect(patches).toHaveLength(1);
    expect(screen.getByText('End this for good?')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'End it' }));
    await waitFor(() => expect(patches).toHaveLength(2));
    expect(patches[1]?.body).toEqual({ status: 'ended' });
  });

  it('shows a stale offer as a message and reloads the list on 412', async () => {
    let gets = 0;
    renderAt('/offers', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.method === 'PATCH' ? problem(412, 'precondition_failed') : undefined),
        (c) => (c.method === 'GET' && c.path === `${BASE}/offers` ? ((gets += 1), json(200, { items: [offer('o1')], nextCursor: null })) : undefined),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    expect(await screen.findByText(/changed on another device/)).toBeInTheDocument();
    await waitFor(() => expect(gets).toBeGreaterThan(1));
  });

  it('creates a percent offer with a priced preview and one Idempotency-Key', async () => {
    const posts: Call[] = [];
    renderAt('/offers', {
      messages,
      handlers: [
        shops('owner'),
        (c) => {
          if (c.method === 'POST' && c.path === `${BASE}/offers`) {
            posts.push(c);
            return json(201, offer('o9'));
          }
          return undefined;
        },
        preview,
        items([item('i1')]),
        offers([]),
      ],
    });
    fireEvent.click((await screen.findAllByRole('button', { name: 'New offer' }))[0] as HTMLElement);
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create offer' }));
    expect(await within(sheet).findByText('Choose at least one item.')).toBeInTheDocument();
    expect(posts).toHaveLength(0);

    fireEvent.click(await within(sheet).findByRole('button', { name: 'Add' }));
    fireEvent.change(within(sheet).getByLabelText('Percent off'), { target: { value: '95' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create offer' }));
    expect(await within(sheet).findByText('Enter a whole number from 1 to 90.')).toBeInTheDocument();

    fireEvent.change(within(sheet).getByLabelText('Percent off'), { target: { value: '20' } });
    expect(await within(sheet).findByText('With the offer', {}, { timeout: 3000 })).toBeInTheDocument();
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create offer' }));
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0]?.body).toMatchObject({ kind: 'percent', value: 20, scope: 'items', storeItemIds: ['i1'], channels: 'both' });
    expect(posts[0]?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByText('Offer created')).toBeInTheDocument();
  });

  it('opens a deep link prefilled and creates nothing; unknown items open nothing', async () => {
    const posts: Call[] = [];
    const watch: Handler = (c) => (c.method === 'POST' && c.path === `${BASE}/offers` ? (posts.push(c), undefined) : undefined);
    const one: Handler = (c) => (c.method === 'GET' && c.path === `${BASE}/items/i1` ? json(200, item('i1')) : undefined);
    renderAt('/offers?offer=item&item=i1&pct=15', { messages, handlers: [shops('owner'), watch, one, preview, items([item('i1')]), offers([])] });
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).getByLabelText('Percent off')).toHaveValue('15');
    expect(await within(sheet).findByText('Item i1 · 1 kg')).toBeInTheDocument();
    expect(posts).toHaveLength(0);
  });

  it('ignores a deep link to an item this shop does not have', async () => {
    renderAt('/offers?offer=item&item=zzz&pct=15', { messages, handlers: [shops('owner'), (c) => (c.path.endsWith('/items/zzz') ? problem(404, 'not_found') : undefined), items([]), offers([])] });
    expect(await screen.findByText('No offers yet')).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('makes one flash offer per ticked batch', async () => {
    const posts: Call[] = [];
    renderAt('/offers', {
      messages,
      handlers: [
        shops('owner'),
        (c) => {
          if (c.method === 'POST' && c.path === `${BASE}/offers`) {
            posts.push(c);
            return json(201, offer(`o${posts.length}`, { source: 'flash_expiry' }));
          }
          if (c.method === 'GET' && c.path === `${BASE}/batches`) {
            const b = (id: string, itemId: string, exp: string) => ({ id, etag: '"v1"', itemId, itemName: `Item ${itemId}`, receivedQuantity: 10, quantity: 4, expiresOn: exp, expired: false, receivedAt: '2026-10-01T08:00:00.000Z', costPaise: null, foundInCount: false, pricePaise: 5800, mrpPaise: null, updatedAt: '2026-10-01T08:00:00.000Z' });
            return json(200, { items: [b('b1', 'i1', '2026-10-27'), b('b2', 'i2', '2026-10-29')], nextCursor: null });
          }
          return undefined;
        },
        preview,
        items([item('i1'), item('i2')]),
        offers([]),
      ],
    });
    fireEvent.click((await screen.findAllByRole('button', { name: 'New offer' }))[0] as HTMLElement);
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByRole('button', { name: 'Expiring stock' }));
    await within(sheet).findByText('Item i2');
    const boxes = within(sheet).getAllByRole('checkbox');
    fireEvent.click(boxes[0] as HTMLElement);
    fireEvent.click(boxes[1] as HTMLElement);
    fireEvent.change(within(sheet).getByLabelText('Percent off'), { target: { value: '30' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create offer' }));
    await waitFor(() => expect(posts).toHaveLength(2));
    expect(posts.map((p) => (p.body as { batchId: string }).batchId).sort()).toEqual(['b1', 'b2']);
    expect(posts[0]?.headers.get('idempotency-key')).not.toBe(posts[1]?.headers.get('idempotency-key'));
  });
});

describe('B-08 banners', () => {
  it('shows the moderation states and the reach', async () => {
    renderAt('/offers', {
      messages,
      handlers: [
        shops('owner'),
        offers([]),
        banners([banner('a'), banner('b', { status: 'pending_review', moderation: 'needs_review' }), banner('c', { status: 'removed', moderation: 'rejected', removeReason: 'Not a shop photo' })]),
      ],
    });
    fireEvent.click(await screen.findByRole('tab', { name: 'Banners' }));
    expect(await screen.findByText('Banner a')).toBeInTheDocument();
    expect(screen.getAllByText(/120 views · 9 taps · 2 orders/).length).toBe(3);
    expect(screen.getByText('Checking image…')).toBeInTheDocument();
    expect(screen.getByText('Removed by TodayZ: Not a shop photo')).toBeInTheDocument();
  });

  it('creates a template banner and shows the banner limit', async () => {
    const posts: Call[] = [];
    let full = true;
    renderAt('/offers', {
      messages,
      handlers: [
        shops('manager'),
        offers([]),
        banners([]),
        (c) => {
          if (c.method === 'POST' && c.path === `${BASE}/banners`) {
            posts.push(c);
            if (full) {
              full = false;
              return problem(422, 'banner_limit_reached');
            }
            return json(201, banner('n'));
          }
          return undefined;
        },
      ],
    });
    fireEvent.click(await screen.findByRole('tab', { name: 'Banners' }));
    fireEvent.click((await screen.findAllByRole('button', { name: 'New banner' }))[0] as HTMLElement);
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create banner' }));
    expect(await within(sheet).findByText('Enter a headline.')).toBeInTheDocument();
    fireEvent.change(within(sheet).getByLabelText('Headline'), { target: { value: 'Diwali sweets' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Rose' }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create banner' }));
    expect(await within(sheet).findByText(/reached the limit of banners/)).toBeInTheDocument();
    expect(posts[0]?.body).toMatchObject({ headline: 'Diwali sweets', template: { tint: 'rose' }, targetType: 'store' });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Create banner' }));
    await waitFor(() => expect(posts).toHaveLength(2));
    expect(await screen.findByText('Banner created')).toBeInTheDocument();
  });
});

describe('sample cart', () => {
  const pool = [{ id: 'i1', title: 'A', pricePaise: 4000 }, { id: 'i2', title: 'B', pricePaise: 1000 }];
  it('uses the offer items, or the first listed items for a whole-shop offer', () => {
    expect(sampleLines({ kind: 'percent', scope: 'items', storeItemIds: ['i2'] }, [], pool)).toEqual([{ storeItemId: 'i2', quantity: 1 }]);
    expect(sampleLines({ kind: 'percent', scope: 'store' }, [], pool).map((l) => l.storeItemId)).toEqual(['i1', 'i2']);
  });
  it('adds units for buy X get Y and reaches a minimum bill', () => {
    expect(sampleLines({ kind: 'bxgy', scope: 'items', storeItemIds: ['i1'], buyQty: 2, getQty: 1 }, [], pool)[0]?.quantity).toBe(3);
    expect(sampleLines({ kind: 'bill_min', scope: 'store', minBillPaise: 10000 }, [], pool)[0]?.quantity).toBe(Math.ceil((10000 - 1000) / 4000));
  });
});

// The page's own store gate is a status with the same name; the list's one is busy and holds a skeleton.
const listBusy = () =>
  screen.queryAllByRole('status', { name: 'Loading offers' }).some((el) => el.getAttribute('aria-busy') === 'true' && el.querySelector('.tz-ui-skeleton') !== null);

describe('B-08 loading states', () => {
  it('shows a skeleton while the offers load', async () => {
    renderAt('/offers', { messages, handlers: [shops('owner'), holding((c) => c.path === `${BASE}/offers`)] });
    await waitFor(() => expect(listBusy()).toBe(true));
  });

  it('shows a skeleton while the banners load', async () => {
    renderAt('/offers', { messages, handlers: [shops('owner'), offers([]), holding((c) => c.path === `${BASE}/banners`)] });
    fireEvent.click(await screen.findByRole('tab', { name: 'Banners' }));
    await waitFor(() => expect(listBusy()).toBe(true));
  });
});

describe('B-08 sheet choices while their options load', () => {
  const cats = (c: Call) => c.method === 'GET' && c.path === '/v1/discover/categories';
  const busyIn = (el: HTMLElement) =>
    within(el).queryAllByRole('status', { name: 'Loading' }).some((s) => s.getAttribute('aria-busy') === 'true' && s.querySelector('.tz-ui-skeleton') !== null);
  async function offerSheet(handlers: Handler[]) {
    renderAt('/offers', { messages, handlers: [shops('owner'), offers([]), ...handlers] });
    fireEvent.click((await screen.findAllByRole('button', { name: 'New offer' }))[0] as HTMLElement);
    return screen.findByRole('dialog');
  }

  it('offer sheet: the category choice is a skeleton while categories load, never an empty list', async () => {
    const sheet = await offerSheet([holding(cats)]);
    fireEvent.click(within(sheet).getByRole('radio', { name: 'Category' }));
    await waitFor(() => expect(busyIn(sheet)).toBe(true));
    expect(within(sheet).queryByRole('combobox', { name: 'Category' })).toBeNull();
  });

  it('offer sheet: a failed category list says so, and Try again shows the loading state', async () => {
    // The categories query retries once by itself, so two failures reach the error.
    const list = failThenHold(cats, 2);
    const sheet = await offerSheet([list]);
    fireEvent.click(within(sheet).getByRole('radio', { name: 'Category' }));
    expect(await within(sheet).findByText('Check your connection and try again.', {}, { timeout: 4000 })).toBeInTheDocument();
    fireEvent.click(within(sheet).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(list.calls()).toBe(3));
    await waitFor(() => expect(busyIn(sheet) || within(sheet).queryByRole('button', { name: 'Try again' })?.getAttribute('aria-busy') === 'true').toBe(true));
  });

  it('offer sheet: the expiring batches show a skeleton while they load', async () => {
    const sheet = await offerSheet([holding((c) => c.method === 'GET' && c.path === `${BASE}/batches`)]);
    fireEvent.click(within(sheet).getByRole('button', { name: 'Expiring stock' }));
    await waitFor(() => expect(busyIn(sheet)).toBe(true));
  });

  it('banner sheet: the category choice is a skeleton while categories load', async () => {
    renderAt('/offers', { messages, handlers: [shops('owner'), offers([]), banners([]), holding(cats)] });
    fireEvent.click(await screen.findByRole('tab', { name: 'Banners' }));
    fireEvent.click((await screen.findAllByRole('button', { name: 'New banner' }))[0] as HTMLElement);
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByRole('radio', { name: 'A category' }));
    await waitFor(() => expect(busyIn(sheet)).toBe(true));
    expect(within(sheet).queryByRole('combobox', { name: 'Category' })).toBeNull();
  });
});
