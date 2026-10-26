import { describe, expect, it } from 'vitest';
import { effectiveMrp, itemTitle, parseQuantity, parseRupees, rupeesText, stockLevel, withBrand } from './format';

describe('parseQuantity', () => {
  it('accepts whole numbers from 0 to 1,000,000', () => {
    expect(parseQuantity('0')).toBe(0);
    expect(parseQuantity(' 12 ')).toBe(12);
    expect(parseQuantity('1000000')).toBe(1_000_000);
  });
  it('rejects everything else', () => {
    for (const t of ['', '-1', '1.5', '1e3', 'abc', '1000001', '12345678']) expect(parseQuantity(t)).toBeNull();
  });
});

describe('parseRupees', () => {
  it('turns rupees into integer paise', () => {
    expect(parseRupees('58')).toBe(5800);
    expect(parseRupees('58.5')).toBe(5850);
    expect(parseRupees('58.05')).toBe(5805);
    expect(parseRupees('0.99')).toBe(99);
  });
  it('rejects empty, zero, negative and more than two decimals', () => {
    for (const t of ['', '0', '0.00', '-5', '5.123', 'x', '1,000']) expect(parseRupees(t)).toBeNull();
  });
  it('round-trips through rupeesText', () => {
    for (const p of [5800, 5850, 5805, 99]) expect(parseRupees(rupeesText(p))).toBe(p);
  });
});

describe('withBrand', () => {
  it('leads with the brand unless the name already does', () => {
    expect(withBrand('Sugar 1 kg', 'Daawat')).toBe('Daawat Sugar 1 kg');
    expect(withBrand('Daawat Sugar 1 kg', 'Daawat')).toBe('Daawat Sugar 1 kg');
    expect(withBrand('daawat sugar', 'Daawat')).toBe('daawat sugar');
    expect(withBrand('Daawatwala Sugar', 'Daawat')).toBe('Daawat Daawatwala Sugar');
    expect(withBrand('Toor dal', null)).toBe('Toor dal');
  });
});

describe('itemTitle', () => {
  it('adds the variant and pack, but not a pack the name already ends with', () => {
    expect(itemTitle({ name: 'Amul Butter', variant: null, packSize: 100, unit: 'g' })).toBe('Amul Butter · 100 g');
    expect(itemTitle({ name: 'Atta 5kg', variant: 'Whole wheat', packSize: 5, unit: 'kg' })).toBe('Atta 5kg · Whole wheat');
  });
});

describe('stockLevel and effectiveMrp', () => {
  it('is out at zero, low when flagged, else in', () => {
    expect(stockLevel({ available: 0, lowStock: true })).toBe('out');
    expect(stockLevel({ available: 2, lowStock: true })).toBe('low');
    expect(stockLevel({ available: 20, lowStock: false })).toBe('in');
  });
  it("prefers the shop's own MRP", () => {
    expect(effectiveMrp({ mrpPaise: 6000, productMrpPaise: 6500 })).toBe(6000);
    expect(effectiveMrp({ mrpPaise: null, productMrpPaise: 6500 })).toBe(6500);
    expect(effectiveMrp({ mrpPaise: null, productMrpPaise: null })).toBeNull();
  });
});
