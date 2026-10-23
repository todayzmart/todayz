import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { clearCart } from '../cart/cart';
import { AGO_12_MIN, CATEGORIES, NOW, SHOP_B, atBengaluru, card, item, renderDiscover } from './discover-fixtures';

beforeEach(() => clearCart());

const result = (over: Record<string, unknown> = {}) => ({
  query: 'atta',
  summary: { storesChecked: 12, storesWithIt: 2, minPricePaise: 24000 },
  results: [
    { store: card(), items: [item()], badge: 'lowest_price', deliveryFeePaise: 2000 },
    {
      store: card({ storeSlug: SHOP_B, name: 'Annapurna Kirana', distanceM: 1200 }),
      items: [item({ storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM02', pricePaise: 27000, confirmedAt: null })],
      badge: null,
      deliveryFeePaise: 0,
    },
  ],
  chips: [],
  refineHint: null,
  nextCursor: null,
  serverNow: NOW,
  ...over,
});

function routes(r: unknown = result()) {
  return {
    '/v1/discover/search': () => r,
    '/v1/discover/search/suggest': () => ({ items: [] }),
    '/v1/discover/categories': () => CATEGORIES,
  };
}

describe('C-05 Search', () => {
  it('groups results by shop with the proof line, one badge, delivery fee and freshness', async () => {
    const { calls } = renderDiscover('/search?q=atta', routes());
    atBengaluru();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    expect(screen.getByText('Annapurna Kirana')).toBeInTheDocument();
    expect(screen.getByText('Checked 12 shops nearby · 2 have it · from ₹240')).toBeInTheDocument();
    expect(screen.getAllByText('Lowest price').length).toBeGreaterThan(0);
    expect(screen.queryByText('Closest')).not.toBeInTheDocument();
    expect(screen.getByText('+₹20 delivery')).toBeInTheDocument();
    expect(screen.getByText(/Confirmed 12 min/)).toBeInTheDocument();
    // No timestamp from the API means the stale wording, not a guessed time.
    expect(screen.getByText('Stock not confirmed recently')).toBeInTheDocument();
    const search = calls.find((c) => c.path === '/v1/discover/search');
    expect(search?.query.get('q')).toBe('atta');
    expect(search?.query.get('radius')).toBe('2000');
    expect(AGO_12_MIN).toBeTruthy();
  });

  it('hides the summary line and shows the demand line when no shop has it', async () => {
    renderDiscover('/search?q=saffron', routes(result({ summary: { storesChecked: 12, storesWithIt: 0, minPricePaise: null }, results: [] })));
    atBengaluru();
    expect(await screen.findByText(/No shop nearby has .* in stock/)).toBeInTheDocument();
    expect(screen.queryByText(/Checked 12 shops/)).not.toBeInTheDocument();
    expect(screen.getByText(/looking for this/)).toBeInTheDocument();
  });

  it('a filter chip goes into the request', async () => {
    const { calls } = renderDiscover('/search?q=atta', routes());
    atBengaluru();
    await screen.findByText('Sri Lakshmi Stores');
    fireEvent.click(screen.getByRole('button', { name: 'Open now' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/discover/search' && c.query.get('openNow') === 'true')).toBe(true));
  });

  it('adds to the cart from a result', async () => {
    renderDiscover('/search?q=atta', routes());
    atBengaluru();
    await screen.findByText('Sri Lakshmi Stores');
    fireEvent.click(screen.getAllByRole('button', { name: /^Add/ }).at(0) as HTMLElement);
    expect(await screen.findByText(/View cart/)).toBeInTheDocument();
  });

  it('keeps recent searches on this device only', async () => {
    renderDiscover('/search', routes());
    atBengaluru();
    const box = await screen.findByRole('searchbox');
    fireEvent.change(box, { target: { value: 'milk' } });
    fireEvent.submit(box.closest('form') as HTMLFormElement);
    await waitFor(() => expect(localStorage.getItem('todayz.recentSearches')).toContain('milk'));
  });
});
