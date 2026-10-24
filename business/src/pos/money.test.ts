import { describe, expect, it } from 'vitest';
import { amountDue, billTotals, creditsAllowed, lineTotal, parseRupees, roundOff } from './money';

describe('round-off (D-94, POS-16)', () => {
  it.each([
    [10049, -49],
    [10050, 50],
    [10051, 49],
    [10000, 0],
    [10099, 1],
    [10001, -1],
    [49, 0],
    [50, 50],
  ])('₹%i paise rounds by %i', (net, adj) => {
    expect(roundOff(net)).toBe(adj);
  });

  it('stays within -49…+50 for every amount', () => {
    for (let n = 1; n < 5000; n += 1) {
      const a = roundOff(n);
      expect(a).toBeGreaterThanOrEqual(-49);
      expect(a).toBeLessThanOrEqual(50);
      if (n >= 50) expect((n + a) % 100).toBe(0);
    }
  });
});

describe('billTotals', () => {
  const line = (unitPricePaise: number, quantity: number, discountPaise = 0) => ({ unitPricePaise, quantity, discountPaise });

  it('adds lines, applies the bill discount, then rounds', () => {
    const t = billTotals([line(5800, 2), line(12950, 1)], 500);
    expect(t.subtotalPaise).toBe(24550);
    expect(t.billDiscountPaise).toBe(500);
    expect(t.netPaise).toBe(24050);
    expect(t.roundOffPaise).toBe(50);
    expect(t.totalPaise).toBe(24100);
    expect(t.itemCount).toBe(3);
  });

  it('takes a line discount off its own line only', () => {
    expect(lineTotal(line(1000, 3, 500))).toBe(2500);
    expect(lineTotal(line(1000, 1, 5000))).toBe(0);
  });

  it('never lets discounts take the bill below ₹1', () => {
    const t = billTotals([line(5000, 1)], 999_999);
    expect(t.billDiscountPaise).toBe(4900);
    expect(t.netPaise).toBe(100);
    expect(t.totalPaise).toBe(100);
    const free = billTotals([line(5000, 1, 5000)], 0);
    expect(free.totalPaise).toBe(0);
  });

  it('is zero for an empty bill and ignores a negative discount', () => {
    expect(billTotals([], 100).totalPaise).toBe(0);
    expect(billTotals([line(1000, 1)], -50).billDiscountPaise).toBe(0);
  });
});

describe('credits and amounts', () => {
  it('needs the bill after discounts to reach the minimum (D-30)', () => {
    expect(creditsAllowed(9999, 10000)).toBe(false);
    expect(creditsAllowed(10000, 10000)).toBe(true);
  });
  it('subtracts credits from the amount due, never below 0', () => {
    expect(amountDue(48000, 3000)).toBe(45000);
    expect(amountDue(1000, 3000)).toBe(0);
  });
  it('parses rupee amounts', () => {
    expect(parseRupees('12.5')).toBe(1250);
    expect(parseRupees('₹1,200')).toBe(120000);
    expect(parseRupees('12.345')).toBeNull();
    expect(parseRupees('abc')).toBeNull();
    expect(parseRupees('')).toBeNull();
  });
});
