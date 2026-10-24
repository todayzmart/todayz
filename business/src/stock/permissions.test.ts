import { describe, expect, it } from 'vitest';
import { canStock, type StockAction } from './permissions';

const ACTIONS: StockAction[] = ['count', 'restock', 'damage', 'price', 'manage', 'batches', 'addItem', 'import'];

describe('canStock', () => {
  it('lets a cashier count and nothing else', () => {
    expect(ACTIONS.filter((a) => canStock('cashier', a))).toEqual(['count']);
  });
  it('lets managers and owners do everything', () => {
    for (const role of ['manager', 'owner'] as const) expect(ACTIONS.every((a) => canStock(role, a))).toBe(true);
  });
});
