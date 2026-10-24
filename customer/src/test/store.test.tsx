import { fireEvent, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { addToCart, clearCart, getCart } from '../cart/cart';
import { CATEGORIES, NOW, SHOP_A, SHOP_B, STORE_A_ID, STORE_B_ID, atBengaluru, item, page, pageOf, renderDiscover } from './discover-fixtures';
import { problem } from './render';

beforeEach(() => clearCart());

function routes(shop = page(), items = [item(), item({ storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM02', name: 'Toor Dal', packSize: 1, pricePaise: 18000, mrpPaise: null })]) {
  return {
    [`/v1/discover/stores/${shop.storeSlug}`]: () => shop,
    [`/v1/discover/stores/${shop.storeSlug}/items`]: () => pageOf(items),
    '/v1/discover/categories': () => CATEGORIES,
  };
}

describe('C-07 Shop page', () => {
  it('shows the header, today\'s hours and the items with stock freshness', async () => {
    renderDiscover(`/s/${SHOP_A}`, routes(page({ categoryName: 'Kirana' })));
    atBengaluru();
    expect(await screen.findByRole('heading', { level: 1, name: 'Sri Lakshmi Stores' })).toBeInTheDocument();
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText(/^Today /)).toBeInTheDocument();
    // The category name comes from the shop page (the business's category).
    expect(screen.getByText('Kirana')).toBeInTheDocument();
    expect(await screen.findByText('Aashirvaad Atta')).toBeInTheDocument();
    expect(screen.getByText('Toor Dal')).toBeInTheDocument();
    expect(screen.getAllByText(/Confirmed 12 min/).length).toBeGreaterThan(0);
    expect(screen.getByRole('link', { name: /Get directions/ })).toBeInTheDocument();
  });

  it('Add to cart puts the item in the one-shop cart', async () => {
    renderDiscover(`/s/${SHOP_A}`, routes());
    atBengaluru();
    await screen.findByText('Aashirvaad Atta');
    fireEvent.click(screen.getAllByRole('button', { name: /^Add/ }).at(0) as HTMLElement);
    expect(getCart().lines).toHaveLength(1);
    expect(getCart().store?.slug).toBe(SHOP_A);
    // The cart keeps the store ULID (what checkout needs), not the slug.
    expect(getCart().store?.id).toBe(STORE_A_ID);
    expect(getCart().store?.id).not.toBe(SHOP_A);
  });

  it('asks before replacing a cart that holds another shop\'s items', async () => {
    addToCart({ id: STORE_B_ID, slug: SHOP_B, name: 'Annapurna Kirana' }, { storeItemId: 'x1', productName: 'Rice', pricePaise: 5000 }, 1);
    renderDiscover(`/s/${SHOP_A}`, routes());
    atBengaluru();
    await screen.findByText('Aashirvaad Atta');
    fireEvent.click(screen.getAllByRole('button', { name: /^Add/ }).at(0) as HTMLElement);
    expect(await screen.findByText('Start a new cart?')).toBeInTheDocument();
    expect(getCart().store?.slug).toBe(SHOP_B);
    fireEvent.click(screen.getByRole('button', { name: 'Replace cart' }));
    expect(getCart().store?.slug).toBe(SHOP_A);
  });

  it('an in-store-only shop has directions instead of Add', async () => {
    renderDiscover(`/s/${SHOP_A}`, routes(page({ bookable: false, pickup: false, delivery: false })));
    atBengaluru();
    await screen.findByText('Aashirvaad Atta');
    expect(screen.queryByRole('button', { name: /^Add(?! to basket)/ })).not.toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: /Get directions/ }).length).toBeGreaterThan(0);
    expect(screen.getByText(/sells in store only/)).toBeInTheDocument();
  });

  it('keeps the QR code from ?r= on the device', async () => {
    renderDiscover(`/s/${SHOP_A}?r=QR12345`, routes());
    await screen.findByRole('heading', { level: 1, name: 'Sri Lakshmi Stores' });
    expect(localStorage.getItem('todayz.ref')).toContain('QR12345');
    expect(NOW).toBeTruthy();
  });

  it('says so when the shop is not live', async () => {
    renderDiscover(`/s/${SHOP_A}`, { [`/v1/discover/stores/${SHOP_A}`]: () => problem(404, 'not_found') });
    expect(await screen.findByText('This shop is not on TodayZ right now')).toBeInTheDocument();
  });
});
