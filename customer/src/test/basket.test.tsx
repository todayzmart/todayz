// C-22 baskets (W2-14): editor, device basket, results, Fill cart, split and "Next" on C-12, autosave with
// If-Match, the save offer and "Add to basket".
import type { Basket } from '@todayz/api-client/gen/model/basket';
import type { BasketCoveredLine } from '@todayz/api-client/gen/model/basketCoveredLine';
import type { BasketSearchResult } from '@todayz/api-client/gen/model/basketSearchResult';
import type { BasketSearchStore } from '@todayz/api-client/gen/model/basketSearchStore';
import type { Order } from '@todayz/api-client/gen/model/order';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { getDeviceBasket, resetDeviceBasketForTests, setDeviceBasket } from '../basket/device';
import { resetPendingSplitForTests, setPendingSplit } from '../basket/fill';
import { addToCart, getCart, resetCartForTests } from '../cart/cart';
import { setStreamFetchForTests } from '../orders/stream';
import { atBengaluru, item, NOW, page, pageOf, SHOP_A, STORE_A_ID, STORE_B_ID } from './discover-fixtures';
import { json, problem, renderAt, type Call, type Handler } from './render';


const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const BASKET_ID = '01KE0MK0000000000000000001';
const ATTA = '01KE0PR0000000000000000001';
const MILK = '01KE0PR0000000000000000002';
const DOLO = '01KE0PR0000000000000000003';
const ORDER_ID = '01J9ZORDERAAAAAAAAAAAAAAAA';

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on =
  (method: string, path: string, make: (c: Call) => Response): Handler =>
  (c) =>
    c.method === method && c.path === path ? make(c) : undefined;
const quietStream = (() =>
  Promise.resolve(
    new Response(new ReadableStream<Uint8Array>({ start: () => undefined }), { status: 200, headers: { 'content-type': 'text/event-stream' } }),
  )) as typeof fetch;

const devLine = (key: string, productId: string | null, label: string, quantity = 1) => ({
  key,
  productId,
  text: productId ? null : label,
  label,
  detail: '',
  quantity,
});

function basket(over: Partial<Basket> = {}): Basket {
  return {
    basketId: BASKET_ID,
    name: 'Weekly',
    lines: [
      { lineId: 'L1', productId: ATTA, productName: 'Aashirvaad Atta', variant: null, packSize: 5, unit: 'kg', text: null, quantity: 1, position: 0 },
      { lineId: 'L2', productId: MILK, productName: 'Amul Milk', variant: null, packSize: 500, unit: 'ml', text: null, quantity: 2, position: 1 },
    ],
    lastSplit: null,
    updatedAt: NOW,
    version: 3,
    ...over,
  } as Basket;
}

function covered(lineIndex: number, productId: string, name: string, pricePaise: number, quantity = 1): BasketCoveredLine {
  return {
    lineIndex,
    storeItemId: `01KE0SI00000000000000000${lineIndex}${productId.slice(-1)}`,
    productId,
    name,
    variant: null,
    packSize: 1,
    unit: 'kg' as BasketCoveredLine['unit'],
    pricePaise,
    mrpPaise: pricePaise + 1000,
    imageKey: null,
    quantity,
    lineTotalPaise: pricePaise * quantity,
    confirmedAt: '2026-10-23T09:48:00.000Z',
  };
}

function shop(over: Partial<BasketSearchStore> = {}): BasketSearchStore {
  return {
    storeId: STORE_A_ID,
    storeSlug: SHOP_A,
    storeName: 'Sri Lakshmi Stores',
    locality: 'Indiranagar',
    distanceM: 400,
    open: true,
    bookable: true,
    coveredCount: 2,
    lineCount: 2,
    totalPaise: 36000,
    badge: 'has_everything',
    covered: [covered(0, ATTA, 'Aashirvaad Atta', 30000), covered(1, MILK, 'Amul Milk', 3000, 2)],
    missing: [],
    ...over,
  } as BasketSearchStore;
}

function result(over: Partial<BasketSearchResult> = {}): BasketSearchResult {
  return { summary: { storesChecked: 7, storesWithAll: 1 }, results: [shop()], split: null, serverNow: NOW, ...over } as BasketSearchResult;
}

function order(over: Partial<Order> = {}): Order {
  return {
    id: ORDER_ID,
    number: 'R-1042',
    store: { id: STORE_A_ID, slug: SHOP_A, name: 'Sri Lakshmi Stores', addressLine: '12 Main Road', locality: 'Indiranagar' },
    fulfilment: 'pickup',
    status: 'placed',
    statusReason: null,
    statusNote: null,
    lines: [],
    subtotalPaise: 0,
    discountPaise: 0,
    itemSubtotalPaise: 0,
    itemsAfterOffersPaise: 0,
    creditsUsed: 0,
    deliveryFeePaise: 0,
    totalPaise: 0,
    cashbackCredits: 0,
    paymentStatus: 'pay_at_shop',
    paymentMethod: null,
    code: null,
    codeLocked: false,
    canExtend: false,
    askReplyId: null,
    acceptBy: new Date(Date.parse(NOW) + 4 * 60_000).toISOString(),
    partialConfirmBy: null,
    holdUntil: null,
    extendedAt: null,
    placedAt: NOW,
    acceptedAt: null,
    collectedAt: null,
    timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW }],
    version: 1,
    serverNow: NOW,
    ...over,
  } as Order;
}

beforeEach(() => {
  resetDeviceBasketForTests();
  resetPendingSplitForTests();
  resetCartForTests();
  setStreamFetchForTests(quietStream);
});
afterEach(() => {
  setStreamFetchForTests(undefined);
  setPendingSplit(null);
  resetDeviceBasketForTests();
});

describe('C-22 editor (device basket)', () => {
  it('adds free text, merges the same words and removes a line at zero', async () => {
    renderAt('/baskets/device');
    const box = await screen.findByRole('searchbox', { name: 'Add an item' });
    fireEvent.change(box, { target: { value: 'Toor dal' } });
    fireEvent.submit(box.closest('form') as HTMLFormElement);
    fireEvent.change(box, { target: { value: 'toor dal' } });
    fireEvent.submit(box.closest('form') as HTMLFormElement);

    const list = await screen.findByRole('list', { name: 'Items in this basket' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(1);
    expect(within(list).getByText('Any brand')).toBeInTheDocument();
    expect(getDeviceBasket().lines[0]?.quantity).toBe(2);

    fireEvent.click(within(list).getByRole('button', { name: 'Remove Toor dal' }));
    await waitFor(() => expect(getDeviceBasket().lines).toHaveLength(0));
    expect(screen.getByText(/Your basket is empty/)).toBeInTheDocument();
  });

  it('a product suggestion adds a product line', async () => {
    renderAt('/baskets/device', {
      handlers: [
        on('GET', '/v1/discover/search/suggest', () =>
          json(200, { items: [{ kind: 'product', text: 'Aashirvaad Atta', productId: ATTA, brand: null, categorySlug: null }] }),
        ),
      ],
    });
    const box = await screen.findByRole('searchbox', { name: 'Add an item' });
    fireEvent.change(box, { target: { value: 'aashir' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Aashirvaad Atta' }, { timeout: 2000 }));
    await waitFor(() => expect(getDeviceBasket().lines[0]?.productId).toBe(ATTA));
  });

  it('signed out, the basket says it lives on this device with a sign-in link', async () => {
    setDeviceBasket({ name: '', lines: [devLine('a', ATTA, 'Aashirvaad Atta')] });
    renderAt('/baskets');
    expect(await screen.findByText(/kept on this device only/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in to save it' })).toHaveAttribute('href', expect.stringContaining('/sign-in'));
    expect(screen.getByText('1 item')).toBeInTheDocument();
  });
});

describe('C-22 results', () => {
  const searchWith =
    (r: BasketSearchResult, seen: Call[] = []): Handler =>
    (c) => {
      if (c.method === 'POST' && c.path === '/v1/discover/basket-search') {
        seen.push(c);
        return json(200, r);
      }
      return undefined;
    };

  it('searches with the place and lines, then shows the proof line, badge and totals', async () => {
    setDeviceBasket({ name: '', lines: [devLine('a', ATTA, 'Aashirvaad Atta'), devLine('b', null, 'Dolo 650', 2)] });
    const seen: Call[] = [];
    renderAt('/baskets/device', { handlers: [searchWith(result(), seen)] });
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'Search shops' }));

    expect(await screen.findByText('Checked 7 shops nearby · 1 has everything')).toBeInTheDocument();
    const body = seen[0]?.body as { lat: number; lng: number; lines: unknown[] };
    expect(body.lat).toBe(12.97);
    expect(body.lines).toEqual([
      { productId: ATTA, quantity: 1 },
      { text: 'Dolo 650', quantity: 2 },
    ]);
    expect(screen.getByText('Has everything')).toBeInTheDocument();
    expect(screen.getByText(/2 of 2 · .* · ₹360/)).toBeInTheDocument();
    expect(screen.getByText(/Totals are a preview/)).toBeInTheDocument();
  });

  it('Fill cart puts the covered lines in the cart and asks before replacing another shop', async () => {
    addToCart({ id: STORE_B_ID, slug: 'annapurna-kirana', name: 'Annapurna Kirana' }, { storeItemId: 'X', productName: 'Salt', pricePaise: 2000 }, 1);
    setDeviceBasket({ name: '', lines: [devLine('a', ATTA, 'Aashirvaad Atta'), devLine('b', MILK, 'Amul Milk', 2)] });
    const { router } = renderAt('/baskets/device', { handlers: [searchWith(result())] });
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'Search shops' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Fill cart' }));

    fireEvent.click(await screen.findByRole('button', { name: 'Replace cart' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/cart'));
    const cart = getCart();
    expect(cart.store?.id).toBe(STORE_A_ID);
    expect(cart.lines.map((l) => [l.productName, l.qty])).toEqual([
      ['Aashirvaad Atta', 1],
      ['Amul Milk', 2],
    ]);
  });

  it('with no full match, the split card fills the first shop and keeps the second for later', async () => {
    setDeviceBasket({ name: '', lines: [devLine('a', ATTA, 'Aashirvaad Atta'), devLine('b', DOLO, 'Dolo 650')] });
    const a = shop({ coveredCount: 1, totalPaise: 30000, badge: 'closest', covered: [covered(0, ATTA, 'Aashirvaad Atta', 30000)], missing: [{ lineIndex: 1 }] });
    const b = shop({
      storeId: STORE_B_ID,
      storeSlug: 'annapurna-kirana',
      storeName: 'Annapurna Kirana',
      coveredCount: 1,
      totalPaise: 3200,
      badge: 'lowest_price',
      covered: [covered(1, DOLO, 'Dolo 650', 3200)],
      missing: [{ lineIndex: 0 }],
    });
    const split = {
      stores: [
        { storeId: STORE_A_ID, storeSlug: SHOP_A, storeName: 'Sri Lakshmi Stores', distanceM: 400, bookable: true, lineIndexes: [0], totalPaise: 30000, covered: a.covered },
        { storeId: STORE_B_ID, storeSlug: 'annapurna-kirana', storeName: 'Annapurna Kirana', distanceM: 900, bookable: true, lineIndexes: [1], totalPaise: 3200, covered: b.covered },
      ],
      uncovered: [],
    };
    renderAt('/baskets/device', { handlers: [searchWith(result({ summary: { storesChecked: 5, storesWithAll: 0 }, results: [a, b], split }))] });
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'Search shops' }));

    expect(await screen.findByText('Get it all from two shops')).toBeInTheDocument();
    expect(screen.getAllByText(/Missing: Dolo 650/).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: 'Fill cart from Sri Lakshmi Stores' }));
    await waitFor(() => expect(getCart().store?.id).toBe(STORE_A_ID));
    const pending = JSON.parse(localStorage.getItem('todayz.basket.nextSplit') ?? 'null') as { afterStoreId: string; store: { id: string } };
    expect(pending.afterStoreId).toBe(STORE_A_ID);
    expect(pending.store.id).toBe(STORE_B_ID);
  });
});

describe('C-12 next from a split', () => {
  const pending = () =>
    setPendingSplit({
      afterStoreId: STORE_A_ID,
      store: { id: STORE_B_ID, slug: 'annapurna-kirana', name: 'Annapurna Kirana' },
      lines: [{ storeItemId: 'SI-DOLO', productName: 'Dolo 650', pricePaise: 3200, qty: 1 }],
    });

  it('offers the rest from the second shop on the first shop’s order and fills the cart', async () => {
    pending();
    const { router } = renderAt(`/orders/${ORDER_ID}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order()))],
    });
    expect(await screen.findByRole('heading', { name: 'Next: the rest from Annapurna Kirana' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Fill cart from Annapurna Kirana' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/cart'));
    expect(getCart().store?.id).toBe(STORE_B_ID);
    expect(localStorage.getItem('todayz.basket.nextSplit')).toBeNull();
  });

  it('is not offered on a cancelled order', async () => {
    pending();
    renderAt(`/orders/${ORDER_ID}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order({ status: 'cancelled' })))],
    });
    await screen.findAllByText(/Sri Lakshmi Stores/);
    expect(screen.queryByText(/Next: the rest from/)).not.toBeInTheDocument();
  });
});

describe('C-22 saved basket', () => {
  it('saves a change with If-Match and reloads on 412', async () => {
    let current = basket();
    const patches: Call[] = [];
    let conflict = true;
    renderAt(`/baskets/${BASKET_ID}`, {
      returning: true,
      handlers: [
        signedIn,
        on('GET', `/v1/me/baskets/${BASKET_ID}`, () => json(200, current)),
        on('PATCH', `/v1/me/baskets/${BASKET_ID}`, (c) => {
          patches.push(c);
          if (conflict) {
            conflict = false;
            current = basket({ version: 4, name: 'Weekly (phone)' });
            return problem(412, 'precondition_failed');
          }
          current = basket({ version: 5 });
          return json(200, current);
        }),
      ],
    });
    const list = await screen.findByRole('list', { name: 'Items in this basket' });
    fireEvent.click(within(list).getByRole('button', { name: 'Add one, Amul Milk' }));

    await waitFor(() => expect(patches).toHaveLength(1), { timeout: 3000 });
    expect(patches[0]?.headers.get('If-Match')).toBe('"v3"');
    expect((patches[0]?.body as { lines: { quantity: number }[] }).lines[1]?.quantity).toBe(3);
    expect(await screen.findByText(/changed on another device/)).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: 'Weekly (phone)' })).toBeInTheDocument();
  });

  it('the list shows each basket with "Search again" and deletes with an undo', async () => {
    const deletes: Call[] = [];
    renderAt('/baskets', {
      returning: true,
      handlers: [
        signedIn,
        on('GET', '/v1/me/baskets', () => json(200, { baskets: [{ basketId: BASKET_ID, name: 'Weekly', lineCount: 2, updatedAt: NOW, version: 3 }] })),
        on('DELETE', `/v1/me/baskets/${BASKET_ID}`, (c) => {
          deletes.push(c);
          return new Response(null, { status: 204 });
        }),
      ],
    });
    expect(await screen.findByRole('link', { name: 'Weekly' })).toHaveAttribute('href', `/baskets/${BASKET_ID}`);
    expect(screen.getByRole('link', { name: 'Search again' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Delete Weekly' }));
    expect(screen.queryByRole('link', { name: 'Weekly' })).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    expect(await screen.findByRole('link', { name: 'Weekly' })).toBeInTheDocument();
    expect(deletes).toHaveLength(0);
  });

  it('New basket at the limit says so', async () => {
    renderAt('/baskets', {
      returning: true,
      handlers: [
        signedIn,
        on('GET', '/v1/me/baskets', () => json(200, { baskets: [] })),
        on('POST', '/v1/me/baskets', () => problem(422, 'basket_limit_reached')),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'New basket' }));
    expect(await screen.findByText(/most baskets you can save/)).toBeInTheDocument();
  });

  it('once signed in, the device basket is offered for saving and moves to the account', async () => {
    setDeviceBasket({ name: 'Party', lines: [devLine('a', ATTA, 'Aashirvaad Atta'), devLine('b', null, 'Paper plates', 3)] });
    const posts: Call[] = [];
    const { router } = renderAt('/baskets', {
      returning: true,
      handlers: [
        signedIn,
        on('GET', '/v1/me/baskets', () => json(200, { baskets: [] })),
        on('POST', '/v1/me/baskets', (c) => {
          posts.push(c);
          return json(201, basket({ name: 'Party' }));
        }),
        on('GET', `/v1/me/baskets/${BASKET_ID}`, () => json(200, basket({ name: 'Party' }))),
      ],
    });
    expect(await screen.findByText('You have a basket on this device')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/baskets/${BASKET_ID}`));
    expect(posts[0]?.body).toEqual({
      name: 'Party',
      lines: [
        { productId: ATTA, quantity: 1 },
        { text: 'Paper plates', quantity: 3 },
      ],
    });
    expect(getDeviceBasket().lines).toHaveLength(0);
  });
});

describe('Add to basket', () => {
  const productPage: Handler[] = [
    (c) => (c.path === `/v1/discover/stores/${SHOP_A}` ? json(200, page()) : undefined),
    (c) => (c.path === `/v1/discover/stores/${SHOP_A}/items` ? json(200, pageOf([item()])) : undefined),
  ];

  it('signed out, adds to the basket on this device from the product page', async () => {
    renderAt(`/s/${SHOP_A}/i/${item().storeItemId}`, {
      handlers: [
        ...productPage,
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Add to basket' }));
    await waitFor(() => expect(getDeviceBasket().lines).toHaveLength(1));
    expect(getDeviceBasket().lines[0]?.productId).toBe(item().productId);
    expect(await screen.findByText(/Added .* to Basket on this device/)).toBeInTheDocument();
  });

  it('signed in, picks a saved basket and adds one more with If-Match', async () => {
    const patches: Call[] = [];
    renderAt(`/s/${SHOP_A}/i/${item().storeItemId}`, {
      returning: true,
      handlers: [
        signedIn,
        ...productPage,
        on('GET', '/v1/me/baskets', () => json(200, { baskets: [{ basketId: BASKET_ID, name: 'Weekly', lineCount: 2, updatedAt: NOW, version: 3 }] })),
        on('GET', `/v1/me/baskets/${BASKET_ID}`, () => json(200, basket())),
        on('PATCH', `/v1/me/baskets/${BASKET_ID}`, (c) => {
          patches.push(c);
          return json(200, basket({ version: 4 }));
        }),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Add to basket' }));
    fireEvent.click(await screen.findByRole('button', { name: /^Weekly/ }));
    await waitFor(() => expect(patches).toHaveLength(1));
    expect(patches[0]?.headers.get('If-Match')).toBe('"v3"');
    const lines = (patches[0]?.body as { lines: { productId?: string; quantity: number }[] }).lines;
    expect(lines).toHaveLength(3);
    expect(lines[2]).toEqual({ productId: item().productId, quantity: 1 });
  });
});
