import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clearCart } from '../cart/cart';
import { tilesUrlFor } from '../home/tiles';
import { CATEGORIES, NOW, SHOP_A, SHOP_B, STORE_B_ID, atBengaluru, card, page, pageOf, renderDiscover } from './discover-fixtures';

// The test env's IntersectionObserver never fires, so this one says "on screen" at once.
class SeenAtOnce {
  constructor(private readonly cb: IntersectionObserverCallback) {}
  observe(target: Element) { this.cb([{ isIntersecting: true, target } as IntersectionObserverEntry], this as unknown as IntersectionObserver); }
  unobserve() {}
  disconnect() {}
  takeRecords() { return []; }
}

beforeEach(() => {
  clearCart();
  vi.stubGlobal('IntersectionObserver', SeenAtOnce);
});
afterEach(() => vi.unstubAllGlobals());

// Far enough apart that the map does not cluster them at the radius it opens with.
const PINS = [
  { storeSlug: SHOP_A, lat: 12.984, lng: 77.652, open: true, categoryTint: 'green' },
  { storeSlug: SHOP_B, lat: 12.956, lng: 77.628, open: false, categoryTint: null },
];
const SUMMARY = { storesNearby: 2, openNow: 1, productsInRadius: 40, pins: PINS, pinsTruncated: false, serverNow: NOW };
const STORES = [card(), card({ storeId: STORE_B_ID, storeSlug: SHOP_B, name: 'Annapurna Kirana', open: false, distanceM: 1200 })];

function routes(over: Record<string, () => unknown> = {}) {
  return {
    '/v1/discover/summary': () => SUMMARY,
    '/v1/discover/categories': () => CATEGORIES,
    '/v1/discover/stores': () => pageOf(STORES),
    [`/v1/discover/stores/${SHOP_A}`]: () => page(),
    ...over,
  };
}

/**
 * The Home map shows without a tap once its section is near the screen (stubbed above to be at once). The lazy chunk took over the default 1 s under a parallel load run.
 */
async function showHomeMap() {
  await screen.findByRole('button', { name: 'Sri Lakshmi Stores, open' }, { timeout: 4000 });
}

describe('C-03 map card', () => {
  it('shows the map without a tap', async () => {
    renderDiscover('/', routes());
    atBengaluru();
    await showHomeMap();
    expect(screen.queryByRole('button', { name: /Tap to see the map/ })).not.toBeInTheDocument();
  });

  it('draws the summary pins with their shop names and opens the full map', async () => {
    renderDiscover('/', routes());
    atBengaluru();
    await showHomeMap();
    expect(screen.getByRole('button', { name: 'Sri Lakshmi Stores, open' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Annapurna Kirana, closed' })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Open full map' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Shops near you' })).toBeInTheDocument();
  });

  it('a pin tap on Home goes to the shop page', async () => {
    renderDiscover('/', routes({ [`/v1/discover/stores/${SHOP_A}/items`]: () => pageOf([]) }));
    atBengaluru();
    await showHomeMap();
    fireEvent.click(screen.getByRole('button', { name: 'Sri Lakshmi Stores, open' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Sri Lakshmi Stores' })).toBeInTheDocument();
  });

  it('picks the city archive that covers the place', () => {
    expect(tilesUrlFor(12.97, 77.64)).toMatch(/\/bengaluru\.pmtiles$/);
    expect(tilesUrlFor(17.4, 78.45)).toMatch(/\/hyderabad\.pmtiles$/);
    expect(tilesUrlFor(13.08, 80.27)).toMatch(/\/chennai\.pmtiles$/);
    // Far from every city: the nearest archive, not no map.
    expect(tilesUrlFor(28.6, 77.2)).toMatch(/\.pmtiles$/);
  });
});

describe('C-04 full map', () => {
  it('a pin opens a sheet with the shop and a link to its page', async () => {
    renderDiscover('/map', routes());
    atBengaluru();
    fireEvent.click(await screen.findByRole('button', { name: 'Sri Lakshmi Stores, open' }));
    const dialog = await screen.findByRole('dialog', { name: 'Sri Lakshmi Stores' });
    expect(dialog).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'View shop' })).toHaveAttribute('href', `/s/${SHOP_A}`);
    expect(screen.getByRole('link', { name: 'Directions' })).toHaveAttribute('href', expect.stringContaining('google.com/maps'));
  });

  it('the list view shows the same shops', async () => {
    renderDiscover('/map', routes());
    atBengaluru();
    fireEvent.click(await screen.findByRole('radio', { name: 'List' }));
    expect(await screen.findByText('Annapurna Kirana')).toBeInTheDocument();
  });

  it('says so when the map holds fewer pins than shops', async () => {
    renderDiscover('/map', routes({ '/v1/discover/summary': () => ({ ...SUMMARY, pinsTruncated: true }) }));
    atBengaluru();
    expect(await screen.findByText(/shows the nearest 2 shops/)).toBeInTheDocument();
  });

  it('asks for a location when none is chosen', async () => {
    renderDiscover('/map', routes());
    expect((await screen.findAllByText('Set your location')).length).toBeGreaterThan(0);
  });
});

describe('C-06 category page', () => {
  it('lists the shops of the category within the radius', async () => {
    const { calls } = renderDiscover('/c/pharmacy', routes());
    atBengaluru();
    expect(await screen.findByRole('heading', { level: 1, name: 'Shops in Pharmacy' })).toBeInTheDocument();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    const list = calls.find((c) => c.path === '/v1/discover/stores');
    expect(list?.query.get('category')).toBe('pharmacy');
    expect(list?.query.get('radius')).toBe('2000');
  });

  it('a wider radius asks again', async () => {
    const { calls } = renderDiscover('/c/pharmacy', routes());
    atBengaluru();
    await screen.findByText('Sri Lakshmi Stores');
    fireEvent.click(screen.getByRole('button', { name: '5 km' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/discover/stores' && c.query.get('radius') === '5000')).toBe(true));
  });

  it('says so when the category does not exist', async () => {
    renderDiscover('/c/unknown', routes());
    atBengaluru();
    expect(await screen.findByText('We could not find that category')).toBeInTheDocument();
  });

  it('says so when no shop is in the category and offers a wider radius', async () => {
    renderDiscover('/c/pharmacy', routes({ '/v1/discover/stores': () => pageOf([]) }));
    atBengaluru();
    expect(await screen.findByText('No shops in Pharmacy within 2 km')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Search within 5 km' })).toBeInTheDocument();
  });
});
