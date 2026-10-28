import { fireEvent, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { clearCart, getCart } from '../cart/cart';
import { NOW, SHOP_A, SHOP_B, STORE_B_ID, atBengaluru, card, item, page, pageOf, renderDiscover } from './discover-fixtures';

beforeEach(() => clearCart());

const ID = '01KE0S1W6W8ZV7T1C3A6ITEM01';
const STORE_C_ID = '01KE0S1W6W8ZV7T1C3A6MS0003';

function routes() {
  return {
    [`/v1/discover/stores/${SHOP_A}`]: () => page(),
    [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([item()]),
    '/v1/discover/search': () => ({
      query: 'Aashirvaad Atta',
      summary: { storesChecked: 5, storesWithIt: 2, minPricePaise: 25000 },
      results: [
        { store: card(), items: [item()], badge: null, deliveryFeePaise: 2000 },
        { store: card({ storeId: STORE_C_ID, storeSlug: 'ganesh-provisions', name: 'Ganesh Provisions', distanceM: 900 }), items: [item({ storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM08', pricePaise: 24500, inStock: false })], badge: null, deliveryFeePaise: 0 },
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
    const { calls } = renderDiscover(`/s/${SHOP_A}/i/${ID}`, routes());
    atBengaluru();
    expect(await screen.findByText('Other nearby shops')).toBeInTheDocument();
    // The server filters on the master product; the app no longer matches by name alone.
    expect(calls.find((c) => c.path === '/v1/discover/search')?.query.get('productId')).toBe('01KE0S1W6W8ZV7T1C3A6PROD01');
    expect(screen.getByText('Annapurna Kirana')).toBeInTheDocument();
    expect(screen.getByText('₹250')).toBeInTheDocument();
  });

  it('keeps a shop that has run out in the list, after the ones with stock, labelled (D-282)', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${ID}`, routes());
    atBengaluru();
    const list = (await screen.findByText('Other nearby shops')).closest('section') as HTMLElement;
    const rows = await within(list).findAllByRole('listitem');
    expect(rows.map((r) => r.querySelector('b')?.textContent)).toEqual(['Annapurna Kirana', 'Ganesh Provisions']);
    expect(within(rows[1] as HTMLElement).getByText('Out of stock')).toBeInTheDocument();
    expect(within(rows[0] as HTMLElement).queryByText('Out of stock')).not.toBeInTheDocument();
  });

  it('credits an open-database photo with its authors, source and licence; a shop photo has no credit line (D-252)', async () => {
    const credit = {
      author: 'Open Food Facts contributors',
      licence: 'CC BY-SA 3.0',
      licenceUrl: 'https://creativecommons.org/licenses/by-sa/3.0/',
      sourceUrl: 'https://world.openfoodfacts.org/product/8901262010016',
    };
    const { container, unmount } = renderDiscover(`/s/${SHOP_A}/i/${ID}`, {
      ...routes(),
      [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([item({ imageKey: 'products/p1/off.jpg', imageCredit: credit })]),
    });
    await screen.findByRole('heading', { level: 1, name: 'Aashirvaad Atta' });
    expect(container.querySelector('img')?.getAttribute('src')).toMatch(/\/products\/p1\/off\.jpg$/);
    expect(screen.getByRole('link', { name: 'Open Food Facts contributors' })).toHaveAttribute('href', credit.sourceUrl);
    expect(screen.getByRole('link', { name: 'CC BY-SA 3.0' })).toHaveAttribute('href', credit.licenceUrl);
    unmount();

    renderDiscover(`/s/${SHOP_A}/i/${ID}`, {
      ...routes(),
      [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([item({ imageKey: 'products/p1/shop.jpg', imageCredit: null })]),
    });
    await screen.findByRole('heading', { level: 1, name: 'Aashirvaad Atta' });
    expect(screen.queryByText(/^Photo:/)).not.toBeInTheDocument();
  });

  it('says so when the item is not listed', async () => {
    renderDiscover(`/s/${SHOP_A}/i/${ID}`, {
      [`/v1/discover/stores/${SHOP_A}`]: () => page(),
      [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([]),
    });
    expect(await screen.findByText('We could not find this item')).toBeInTheDocument();
  });
});
