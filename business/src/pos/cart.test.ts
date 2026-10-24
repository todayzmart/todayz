import { describe, expect, it } from 'vitest';
import { cartReducer, cartTotals, emptyCart, MAX_QUANTITY, shortLines, toSaleLines, type CartAction, type CartState } from './cart';
import type { CatalogueItem } from './types';

const rice: CatalogueItem = { id: 'rice', name: 'Rice', variant: '1 kg', packSize: 1, unit: 'kg', barcodes: [], pricePaise: 6050, available: 3, isListed: true };
const oil: CatalogueItem = { id: 'oil', name: 'Oil', packSize: 1, unit: 'l', barcodes: [], pricePaise: 20000, offerPricePaise: 18000, available: 10, isListed: true };

const run = (...actions: CartAction[]): CartState => actions.reduce(cartReducer, emptyCart);

describe('cart lines', () => {
  it('adds an item once and raises the quantity when it is added again', () => {
    const s = run({ type: 'add', item: rice, lineId: 'l1' }, { type: 'add', item: rice, lineId: 'l2' });
    expect(s.lines).toHaveLength(1);
    expect(s.lines[0]).toMatchObject({ id: 'l1', quantity: 2, name: 'Rice 1 kg', unitPricePaise: 6050 });
  });

  it('uses the offer price from the catalogue', () => {
    expect(run({ type: 'add', item: oil, lineId: 'l1' }).lines[0]?.unitPricePaise).toBe(18000);
  });

  it('steps up and down, and removes the line at zero', () => {
    let s = run({ type: 'add', item: rice, lineId: 'l1' }, { type: 'step', lineId: 'l1', by: 1 });
    expect(s.lines[0]?.quantity).toBe(2);
    s = cartReducer(cartReducer(s, { type: 'step', lineId: 'l1', by: -1 }), { type: 'step', lineId: 'l1', by: -1 });
    expect(s.lines).toHaveLength(0);
  });

  it('sets a typed quantity, whole numbers only, capped', () => {
    const s = run({ type: 'add', item: rice, lineId: 'l1' }, { type: 'setQuantity', lineId: 'l1', quantity: 4.9 });
    expect(s.lines[0]?.quantity).toBe(4);
    expect(cartReducer(s, { type: 'setQuantity', lineId: 'l1', quantity: 100000 }).lines[0]?.quantity).toBe(MAX_QUANTITY);
    expect(cartReducer(s, { type: 'setQuantity', lineId: 'l1', quantity: 0 }).lines).toHaveLength(0);
  });

  it('adds a custom line and refuses an empty name', () => {
    const s = run({ type: 'addCustom', lineId: 'c1', name: ' Carry bag ', unitPricePaise: 500 });
    expect(s.lines[0]).toMatchObject({ storeItemId: null, name: 'Carry bag', unitPricePaise: 500 });
    expect(cartReducer(emptyCart, { type: 'addCustom', lineId: 'c2', name: '  ', unitPricePaise: 500 }).lines).toHaveLength(0);
  });

  it('removes a line and clears the cart but keeps the payment method', () => {
    const s = run({ type: 'payment', method: 'upi' }, { type: 'add', item: rice, lineId: 'l1' }, { type: 'billDiscount', discountPaise: 100 });
    expect(cartReducer(s, { type: 'remove', lineId: 'l1' }).lines).toHaveLength(0);
    expect(cartReducer(s, { type: 'clear' })).toEqual({ ...emptyCart, paymentMethod: 'upi' });
  });
});

describe('totals', () => {
  it('rounds the bill to the nearest rupee, half up', () => {
    // ₹60.50 × 2 = ₹121.00; ₹60.50 × 1 = ₹60.50 -> ₹61 (+50)
    const one = run({ type: 'add', item: rice, lineId: 'l1' });
    expect(cartTotals(one)).toMatchObject({ subtotalPaise: 6050, roundOffPaise: 50, totalPaise: 6100 });
    const two = cartReducer(one, { type: 'step', lineId: 'l1', by: 1 });
    expect(cartTotals(two)).toMatchObject({ subtotalPaise: 12100, roundOffPaise: 0, totalPaise: 12100 });
  });

  it('applies line and bill discounts and keeps the bill at ₹1 or more', () => {
    const s = run(
      { type: 'add', item: oil, lineId: 'l1' },
      { type: 'lineDiscount', lineId: 'l1', discountPaise: 3000 },
      { type: 'billDiscount', discountPaise: 99999999 },
    );
    expect(cartTotals(s)).toMatchObject({ subtotalPaise: 15000, billDiscountPaise: 14900, totalPaise: 100 });
  });

  it('turns the cart into sale lines with their totals', () => {
    const s = run({ type: 'add', item: rice, lineId: 'l1', quantity: 2 }, { type: 'lineDiscount', lineId: 'l1', discountPaise: 100 });
    expect(toSaleLines(s)).toEqual([{ clientId: 'l1', storeItemId: 'rice', name: 'Rice 1 kg', unitPricePaise: 6050, quantity: 2, discountPaise: 100, lineTotalPaise: 12000 }]);
  });
});

describe('short stock', () => {
  it('lists lines that want more than is on hand, and still allows the sale', () => {
    const s = run({ type: 'add', item: rice, lineId: 'l1', quantity: 5 }, { type: 'add', item: oil, lineId: 'l2' });
    expect(shortLines(s).map((l) => l.id)).toEqual(['l1']);
    expect(cartTotals(s).totalPaise).toBeGreaterThan(0);
  });
});
