import { describe, expect, it } from 'vitest';
import {
  CASHBACK_OPTIONS,
  cashbackCredits,
  deliveryPayload,
  inStoreOnly,
  kmToMetres,
  rupeesToPaise,
  validateDelivery,
  type DeliveryValues,
} from './delivery';

const base: DeliveryValues = { pickup: true, delivery: true, radiusKm: '3', feeRupees: '30', freeAboveRupees: '', minOrderRupees: '100' };

describe('cashback', () => {
  it('offers 0 to 20% in half-percent steps', () => {
    expect(CASHBACK_OPTIONS[0]).toBe(0);
    expect(CASHBACK_OPTIONS.at(-1)).toBe(2000);
    expect(CASHBACK_OPTIONS).toHaveLength(41);
    expect(CASHBACK_OPTIONS.every((bp) => bp % 50 === 0)).toBe(true);
  });
  it('floors the credits like the server: 5% of ₹1,000 is 50, 2.5% of ₹99 is 2', () => {
    expect(cashbackCredits(100_000, 500)).toBe(50);
    expect(cashbackCredits(9_900, 250)).toBe(2);
    expect(cashbackCredits(9_900, 0)).toBe(0);
  });
});

describe('units', () => {
  it('reads rupees as paise and km as metres', () => {
    expect(rupeesToPaise('30')).toBe(3000);
    expect(rupeesToPaise('1,250.50')).toBe(125050);
    expect(rupeesToPaise('12.345')).toBeNull();
    expect(rupeesToPaise('')).toBeNull();
    expect(kmToMetres('2.5')).toBe(2500);
    expect(kmToMetres('-1')).toBeNull();
  });
});

describe('delivery', () => {
  it('is in-store only when neither pickup nor delivery is on (D-41)', () => {
    expect(inStoreOnly({ pickup: false, delivery: false })).toBe(true);
    expect(inStoreOnly({ pickup: true, delivery: false })).toBe(false);
  });
  it('checks the terms only while delivery is on', () => {
    expect(validateDelivery({ ...base, radiusKm: '0.05' }).radiusKm).toBe('radius');
    expect(validateDelivery({ ...base, radiusKm: '25' }).radiusKm).toBe('radius');
    expect(validateDelivery({ ...base, feeRupees: 'free' }).feeRupees).toBe('money');
    expect(validateDelivery({ ...base, freeAboveRupees: '0' }).freeAboveRupees).toBe('money');
    expect(validateDelivery({ ...base, delivery: false, radiusKm: 'x' })).toEqual({});
  });
  it('sends only what changed, and null clears free-above', () => {
    expect(deliveryPayload(base, base)).toEqual({});
    expect(deliveryPayload({ ...base, feeRupees: '40' }, base)).toEqual({ deliveryFeePaise: 4000 });
    expect(deliveryPayload({ ...base, freeAboveRupees: '500' }, base)).toEqual({ deliveryFreeAbovePaise: 50000 });
    expect(deliveryPayload(base, { ...base, freeAboveRupees: '500' })).toEqual({ deliveryFreeAbovePaise: null });
    expect(deliveryPayload({ ...base, delivery: false, feeRupees: '99' }, base)).toEqual({ acceptsDelivery: false });
  });
});
