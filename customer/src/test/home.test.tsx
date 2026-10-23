import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CATEGORIES, NOW, SHOP_B, STORE_B_ID, atBengaluru, card, pageOf, renderDiscover } from './discover-fixtures';


const SUMMARY = { storesNearby: 14, openNow: 9, productsInRadius: 1280, pins: [], pinsTruncated: false, serverNow: NOW };

function routes(stores = [card(), card({ storeId: STORE_B_ID, storeSlug: SHOP_B, name: 'Annapurna Kirana', open: false, bookable: false, delivery: false, distanceM: 1200 })]) {
  return {
    '/v1/discover/summary': () => SUMMARY,
    '/v1/discover/categories': () => CATEGORIES,
    '/v1/discover/stores': () => pageOf(stores),
  };
}

describe('C-03 Home', () => {
  it('asks for a location when none is chosen', async () => {
    renderDiscover('/', routes());
    expect((await screen.findAllByText('Set your location')).length).toBeGreaterThan(0);
  });

  it('shows the stats, the 2 km default radius and the shops near you', async () => {
    const { calls } = renderDiscover('/', routes());
    atBengaluru();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    expect(screen.getByText('14')).toBeInTheDocument();
    expect(screen.getByText('9')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '2 km' })).toHaveAttribute('aria-pressed', 'true');
    const list = calls.find((c) => c.path === '/v1/discover/stores');
    expect(list?.query.get('radius')).toBe('2000');
    // The in-store-only shop says so and is not marked open.
    expect(screen.getByText('Annapurna Kirana')).toBeInTheDocument();
    expect(screen.getByText('In-store only')).toBeInTheDocument();
  });

  it('a wider radius asks the API again and is remembered', async () => {
    const { calls } = renderDiscover('/', routes());
    atBengaluru();
    await screen.findByText('Sri Lakshmi Stores');
    fireEvent.click(screen.getByRole('button', { name: '5 km' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/discover/stores' && c.query.get('radius') === '5000')).toBe(true));
    expect(screen.getByRole('button', { name: '5 km' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('says so when no shop is in the area, and offers a wider radius', async () => {
    renderDiscover('/', routes([]));
    atBengaluru();
    expect(await screen.findByText('No shops here yet')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Search within 5 km' })).toBeInTheDocument();
  });

  it('a category tile filters the shop list', async () => {
    const { calls } = renderDiscover('/?category=pharmacy', routes());
    atBengaluru();
    await screen.findByText('Sri Lakshmi Stores');
    expect(calls.find((c) => c.path === '/v1/discover/stores')?.query.get('category')).toBe('pharmacy');
    expect(within(document.body).getByText('Showing Pharmacy')).toBeInTheDocument();
  });
});
