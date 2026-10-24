import { describe, expect, it } from 'vitest';
import { MAX_FLASH_BATCHES, validateOffersSearch } from './deeplink';
import { OFFER_LIMITS } from './limits';

describe('validateOffersSearch', () => {
  it('reads an item link with a % inside the safe limits (string or number)', () => {
    expect(validateOffersSearch({ offer: 'item', item: 'i1', pct: '15' })).toEqual({ offer: 'item', item: 'i1', pct: 15 });
    expect(validateOffersSearch({ offer: 'item', item: 'i1', pct: 15 })).toEqual({ offer: 'item', item: 'i1', pct: 15 });
  });

  it('ignores a % that is out of range or not a whole number, but keeps the item', () => {
    for (const pct of ['0', String(OFFER_LIMITS.percentMax + 1), '2.5', '-3', 'abc', '1000']) {
      expect(validateOffersSearch({ offer: 'item', item: 'i1', pct })).toEqual({ offer: 'item', item: 'i1' });
    }
  });

  it('needs an item id for an item link, and ignores pct without the item link', () => {
    expect(validateOffersSearch({ offer: 'item' })).toEqual({});
    expect(validateOffersSearch({ offer: 'item', item: 'a b' })).toEqual({});
    expect(validateOffersSearch({ pct: '10' })).toEqual({});
  });

  it('reads flash batches, de-duplicated and capped at 100', () => {
    expect(validateOffersSearch({ offer: 'flash', batches: 'b1,b2,b1, b3' })).toEqual({ offer: 'flash', batches: ['b1', 'b2', 'b3'] });
    const many = Array.from({ length: 150 }, (_, i) => `b${i}`).join(',');
    expect(validateOffersSearch({ offer: 'flash', batches: many }).batches).toHaveLength(MAX_FLASH_BATCHES);
    expect(validateOffersSearch({ offer: 'flash' })).toEqual({});
    expect(validateOffersSearch({ offer: 'flash', batches: '../x' })).toEqual({});
  });

  it('reads a pair of two different items and ignores anything else', () => {
    expect(validateOffersSearch({ offer: 'pair', items: 'a,b' })).toEqual({ offer: 'pair', items: ['a', 'b'] });
    expect(validateOffersSearch({ offer: 'pair', items: 'a,a' })).toEqual({});
    expect(validateOffersSearch({ offer: 'pair', items: 'a' })).toEqual({});
    expect(validateOffersSearch({ offer: 'pair', items: 'a,b,c' })).toEqual({ offer: 'pair', items: ['a', 'b'] });
  });

  it('ignores unknown offers and parameters', () => {
    expect(validateOffersSearch({ offer: 'banner', item: 'i1' })).toEqual({});
    expect(validateOffersSearch({ foo: 'bar' })).toEqual({});
  });
});
