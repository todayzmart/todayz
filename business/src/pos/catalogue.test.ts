import { describe, expect, it } from 'vitest';
import { findByBarcode, hasOffer, searchItems, sellPricePaise, topTiles } from './catalogue';
import type { CatalogueItem } from './types';

const item = (id: string, name: string, over: Partial<CatalogueItem> = {}): CatalogueItem => ({
  id,
  name,
  variant: null,
  packSize: 1,
  unit: 'pc',
  barcodes: [],
  pricePaise: 1000,
  available: 5,
  isListed: true,
  ...over,
});

const items = [
  item('1', 'Aashirvaad atta', { variant: '5 kg', barcodes: ['8901725181208'], topRank: 2 }),
  item('2', 'Atta noodles', { topRank: 9 }),
  item('3', 'Basmati rice', { variant: '1 kg', barcodes: ['8901234567890'], topRank: 1 }),
  item('4', 'Sunflower oil', { offerPricePaise: 900, isListed: false }),
  item('5', 'Café latte mix'),
];

describe('barcode lookup', () => {
  it('finds an item by its exact code, listed or not', () => {
    expect(findByBarcode(items, '8901234567890')?.id).toBe('3');
    expect(findByBarcode(items, ' 8901725181208 ')?.id).toBe('1');
  });
  it('returns null for an unknown or empty code', () => {
    expect(findByBarcode(items, '000')).toBeNull();
    expect(findByBarcode(items, '  ')).toBeNull();
  });
});

describe('search', () => {
  it('puts names that start with the query first, then names that contain it', () => {
    expect(searchItems(items, 'atta').map((i) => i.id)).toEqual(['2', '1']);
  });
  it('matches every word, in any order', () => {
    expect(searchItems(items, 'kg basmati').map((i) => i.id)).toEqual(['3']);
  });
  it('ignores case and accents', () => {
    expect(searchItems(items, 'CAFE').map((i) => i.id)).toEqual(['5']);
  });
  it('finds an exact barcode before anything else, and a barcode ending', () => {
    expect(searchItems(items, '8901234567890')[0]?.id).toBe('3');
    expect(searchItems(items, '567890').map((i) => i.id)).toEqual(['3']);
  });
  it('breaks a tie by best-seller rank', () => {
    const tied = [item('a', 'Tea red', { topRank: 5 }), item('b', 'Tea green', { topRank: 1 }), item('c', 'Tea black')];
    expect(searchItems(tied, 'tea').map((i) => i.id)).toEqual(['b', 'a', 'c']);
  });
  it('returns nothing for an empty query and respects the limit', () => {
    expect(searchItems(items, '  ')).toEqual([]);
    expect(searchItems(items, 'a', 2)).toHaveLength(2);
  });
});

describe('prices and tiles', () => {
  it('charges the offer price only when it is lower than the shelf price', () => {
    expect(sellPricePaise(items[3] as CatalogueItem)).toBe(900);
    expect(hasOffer(items[3] as CatalogueItem)).toBe(true);
    expect(sellPricePaise(item('x', 'x', { offerPricePaise: 1500 }))).toBe(1000);
    expect(hasOffer(items[0] as CatalogueItem)).toBe(false);
  });
  it('lists the best sellers best first and caps the count', () => {
    expect(topTiles(items).map((i) => i.id)).toEqual(['3', '1', '2']);
    expect(topTiles(items, 2).map((i) => i.id)).toEqual(['3', '1']);
  });
  it('falls back to listed items in stock, alphabetically, while nothing is ranked', () => {
    const plain = [
      item('a', 'Tea'),
      item('b', 'Atta', { available: 0 }),
      item('c', 'Salt'),
      item('d', 'Oil', { isListed: false }),
    ];
    expect(topTiles(plain).map((i) => i.id)).toEqual(['c', 'a']);
  });
});
