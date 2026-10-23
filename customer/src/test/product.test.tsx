import { fireEvent, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { clearCart, getCart } from '../cart/cart';
import { NOW, SHOP_A, SHOP_B, STORE_B_ID, atBengaluru, card, item, page, pageOf, renderDiscover } from './discover-fixtures';

beforeEach(() => clearCart());

const ID = '01KE0S1W6W8ZV7T1C3A6ITEM01';

function routes() {
  return {
    [`/v1/discover/stores/${SHOP_A}`]: () => page(),
    [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([item()]),
    '/v1/discover/search': () => ({
      query: 'Aashirvaad Atta',
      summary: { storesChecked: 5, storesWithIt: 2, minPricePaise: 25000 },
      results: [
        { store: card(), items: [item()], badge: null, deliveryFeePaise: 2000 },
        { store: card({ storeId: STORE_B_ID, storeSlug: SHOP_B, name: 'Annapurna Kirana', distanceM: 1200 }), items: [item({ storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM09', pricePaise: 25000 })], badge: null, deliveryFeePaise: 0 },
      ],
      chips: [],
      refineHint: null,
      nextCursor: null,
      serverNow: NOW,
    }),
  };
}

describe('C-08 Product detail', () => {
  it('shows the item with price, MRP, freshness and the shop, and adds the chosen quantity', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${ID}`, routes());
    atBengaluru();
    expect(await screen.findByRole('heading', { level: 1, name: 'Aashirvaad Atta' })).toBeInTheDocument();
    expect(screen.getByText('₹260')).toBeInTheDocument();
    expect(screen.getByText('₹295')).toBeInTheDocument();
    expect(screen.getByText(/Confirmed 12 min/)).toBeInTheDocument();
    expect(screen.getAllByText('Sri Lakshmi Stores').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: /^Add one/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Add to cart' }));
    expect(getCart().lines[0]?.qty).toBe(2);
  });

  it('lists other nearby shops that have the same product', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${ID}`, routes());
    atBengaluru();
    expect(await screen.findByText('Other nearby shops')).toBeInTheDocument();
    expect(screen.getByText('Annapurna Kirana')).toBeInTheDocument();
    expect(screen.getByText('₹250')).toBeInTheDocument();
  });

  it('says so when the item is not listed', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${ID}`, {
      [`/v1/discover/stores/${SHOP_A}`]: () => page(),
      [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([]),
    });
    expect(await screen.findByText('We could not find this item')).toBeInTheDocument();
  });
});
