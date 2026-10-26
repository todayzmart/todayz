import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import type { DiscoverSubstitutes } from '@todayz/api-client/gen/model/discoverSubstitutes';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { addToCart, clearCart, getCart } from '../cart/cart';
import { NOW, SHOP_A, SHOP_B, STORE_A_ID, STORE_B_ID, atBengaluru, card, item, page, pageOf, renderDiscover } from './discover-fixtures';
import { json, renderAt, type Call, type Handler } from './render';

// W3-12 AI-C3: rule-based substitutes on an out-of-stock item (C-07 shop page, C-08 product, C-09 cart).

const OUT = '01KE0S1W6W8ZV7T1C3A6ITEM01';
const SIMILAR = '01KE0S1W6W8ZV7T1C3A6ITEM02';
const ELSEWHERE = '01KE0S1W6W8ZV7T1C3A6ITEM09';
const SUBS = `/v1/discover/stores/${SHOP_A}/substitutes`;

beforeEach(() => clearCart());

function subs(over: Partial<DiscoverSubstitutes> = {}): DiscoverSubstitutes {
  return {
    sameShop: [item({ storeItemId: SIMILAR, productId: '01KE0S1W6W8ZV7T1C3A6PROD02', name: 'Pillsbury Atta', pricePaise: 24500 })],
    nearby: [
      {
        store: card({ storeId: STORE_B_ID, storeSlug: SHOP_B, name: 'Annapurna Kirana', distanceM: 1200 }),
        items: [item({ storeItemId: ELSEWHERE, pricePaise: 25500 })],
        badge: null,
        deliveryFeePaise: null,
      },
    ],
    serverNow: NOW,
    ...over,
  };
}

function routes(answer: () => unknown = () => subs(), inStock = false) {
  return {
    [`/v1/discover/stores/${SHOP_A}`]: () => page(),
    [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([item({ inStock })]),
    '/v1/discover/search': () => ({ query: 'x', results: [], chips: [], refineHint: false, nextCursor: null, serverNow: NOW }),
    [SUBS]: answer,
  };
}

const subsCalls = (calls: Call[]) => calls.filter((c) => c.path === SUBS);

describe('AI-C3 substitutes on C-08', () => {
  it('opens a sheet with similar items of the shop and the same item nearby, asked only on open', async () => {
    const { calls } = renderDiscover(`/s/${SHOP_A}/i/${OUT}`, routes());
    atBengaluru();
    const open = await screen.findByRole('button', { name: 'See similar items' });
    expect(subsCalls(calls)).toHaveLength(0);
    fireEvent.click(open);
    const sheet = await screen.findByRole('dialog', { name: 'Instead of Aashirvaad Atta' });
    expect(await within(sheet).findByText('Pillsbury Atta')).toBeInTheDocument();
    expect(within(sheet).getByText('In this shop')).toBeInTheDocument();
    expect(within(sheet).getByText('Same item at other shops')).toBeInTheDocument();
    expect(within(sheet).getByText('Annapurna Kirana')).toBeInTheDocument();
    const q = subsCalls(calls)[0]?.query;
    expect(q?.get('itemId')).toBe(OUT);
    expect(q?.get('lat')).toBe('12.97');
    expect(q?.get('lng')).toBe('77.64');
    // Nothing joins the cart from the sheet; a row opens that item's page.
    fireEvent.click(within(sheet).getByRole('link', { name: /Pillsbury Atta/ }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Pillsbury Atta' })).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('says so when nothing similar is in stock', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${OUT}`, routes(() => subs({ sameShop: [], nearby: [] })));
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'See similar items' }));
    expect(await screen.findByText('Nothing similar is in stock nearby.')).toBeInTheDocument();
  });

  it('offers a retry when the request fails', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${OUT}`, routes(() => json(500, { code: 'internal' })));
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'See similar items' }));
    expect(await screen.findByText('Similar items could not load.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });

  it('is not offered on an in-stock item', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${OUT}`, routes(undefined, true));
    atBengaluru();
    expect(await screen.findByRole('button', { name: 'Add to cart' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'See similar items' })).toBeNull();
  });

  it('is hidden without a place', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${OUT}`, routes());
    expect(await screen.findByRole('heading', { level: 1, name: 'Aashirvaad Atta' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'See similar items' })).toBeNull();
  });
});

describe('AI-C3 substitutes on C-07', () => {
  it('shows the button under an out-of-stock item card only', async () => {
    renderDiscover(`/s/${SHOP_A}`, {
      [`/v1/discover/stores/${SHOP_A}`]: () => page(),
      [`/v1/discover/stores/${SHOP_A}/items`]: () =>
        pageOf([item({ inStock: false }), item({ storeItemId: SIMILAR, name: 'Pillsbury Atta' })]),
      [SUBS]: () => subs(),
    });
    atBengaluru();
    expect(await screen.findByText('Pillsbury Atta')).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'See similar items' })).toHaveLength(1);
  });
});

describe('AI-C3 substitutes on C-09', () => {
  const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA' });
  const quote = (): CheckoutQuote =>
    ({
      storeId: STORE_A_ID,
      fulfilment: 'pickup',
      lines: [
        {
          storeItemId: OUT,
          productId: '01KE0S1W6W8ZV7T1C3A6PROD01',
          name: 'Aashirvaad Atta',
          variant: null,
          packSize: 5,
          unit: 'kg',
          imageKey: null,
          quantity: 1,
          availableQuantity: 0,
          availability: 'out_of_stock',
          unitPricePaise: 26000,
          mrpPaise: 29500,
          lineSubtotalPaise: 26000,
          discountPaise: 0,
          lineTotalPaise: 26000,
          priceSource: 'catalog',
          validUntil: null,
        },
      ],
      itemSubtotalPaise: 26000,
      totalPaise: 26000,
      serverNow: NOW,
    }) as unknown as CheckoutQuote;
  const handlers: Handler[] = [
    (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined),
    (c) => (c.method === 'POST' && c.path === '/v1/me/checkout/quote' ? json(200, quote()) : undefined),
    (c) => (c.path === SUBS ? json(200, subs()) : undefined),
  ];

  it('offers similar items on a line the shop no longer has', async () => {
    renderAt('/cart', { returning: true, handlers });
    addToCart({ id: STORE_A_ID, slug: SHOP_A, name: 'Sri Lakshmi Stores' }, { storeItemId: OUT, productName: 'Aashirvaad Atta', pricePaise: 26000 }, 1);
    atBengaluru();
    expect(await screen.findByText('Out of stock')).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'See similar items' }));
    const sheet = await screen.findByRole('dialog', { name: 'Instead of Aashirvaad Atta' });
    expect(await within(sheet).findByText('Pillsbury Atta')).toBeInTheDocument();
    // The line keeps its own Remove; the sheet changes nothing in the cart.
    fireEvent.click(within(sheet).getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove item' })).toBeInTheDocument());
    expect(getCart().lines.map((l) => l.storeItemId)).toEqual([OUT]);
  });
});
