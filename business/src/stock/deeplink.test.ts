import { describe, expect, it } from 'vitest';
import { MAX_COUNT_IDS, planExpiry, validateStockSearch } from './deeplink';

describe('validateStockSearch', () => {
  it('keeps a restock link with its quantity', () => {
    expect(validateStockSearch({ restock: 'item1', qty: '12' })).toEqual({ restock: 'item1', qty: 12 });
    expect(validateStockSearch({ restock: 'item1', qty: 12 })).toEqual({ restock: 'item1', qty: 12 });
  });

  it('drops a quantity that is not a whole number in range, and a quantity without restock', () => {
    expect(validateStockSearch({ restock: 'i', qty: '0' })).toEqual({ restock: 'i' });
    expect(validateStockSearch({ restock: 'i', qty: '2.5' })).toEqual({ restock: 'i' });
    expect(validateStockSearch({ restock: 'i', qty: '1000001' })).toEqual({ restock: 'i' });
    expect(validateStockSearch({ qty: '4' })).toEqual({});
  });

  it('reads count ids, de-duplicated and capped at 100', () => {
    expect(validateStockSearch({ count: 'a,b,a, c' })).toEqual({ count: ['a', 'b', 'c'] });
    const many = Array.from({ length: 150 }, (_, i) => `id${i}`).join(',');
    expect(validateStockSearch({ count: many }).count).toHaveLength(MAX_COUNT_IDS);
  });

  it('reads expire with its batches, price with its reference, and batch only together with item', () => {
    expect(validateStockSearch({ expire: 'i', batches: 'b1,b2' })).toEqual({ expire: 'i', batches: ['b1', 'b2'] });
    expect(validateStockSearch({ price: 'i', ref: '5800' })).toEqual({ price: 'i', ref: 5800 });
    expect(validateStockSearch({ batch: 'b' })).toEqual({});
    expect(validateStockSearch({ batch: 'b', item: 'i' })).toEqual({ batch: 'b', item: 'i' });
  });

  it('add wins over add_q, and add_q is trimmed and capped', () => {
    expect(validateStockSearch({ add: 'p1', add_q: 'milk' })).toEqual({ add: 'p1' });
    expect(validateStockSearch({ add_q: '  milk  ' })).toEqual({ add_q: 'milk' });
    expect(validateStockSearch({ add_q: 'x'.repeat(200) }).add_q).toHaveLength(80);
  });

  it('ignores unknown parameters and malformed ids', () => {
    expect(validateStockSearch({ foo: 'bar', restock: 'a b', price: '<script>', count: '../x' })).toEqual({});
  });
});

describe('planExpiry', () => {
  const batches = [
    { id: 'late', quantity: 5, expiresOn: '2026-12-01' },
    { id: 'early', quantity: 4, expiresOn: '2026-11-01' },
    { id: 'none', quantity: 3, expiresOn: null },
    { id: 'empty', quantity: 0, expiresOn: '2026-10-01' },
  ];

  it('takes the earliest expiry first and only the batches asked for', () => {
    expect(planExpiry(batches, ['late', 'early'], 100)).toEqual([
      { id: 'early', take: 4 },
      { id: 'late', take: 5 },
    ]);
  });

  it('never writes off more than is available', () => {
    expect(planExpiry(batches, ['late', 'early', 'none'], 6)).toEqual([
      { id: 'early', take: 4 },
      { id: 'late', take: 2 },
    ]);
  });

  it('skips unknown and empty batches, and nothing available means an empty plan', () => {
    expect(planExpiry(batches, ['ghost', 'empty'], 10)).toEqual([]);
    expect(planExpiry(batches, ['early'], 0)).toEqual([]);
  });
});
