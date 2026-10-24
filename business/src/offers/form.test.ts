import { describe, expect, it } from 'vitest';
import { checkSchedule, emptyDraft, scopeOf, validateDraft, type ExpiringBatch } from './form';

const NOW = Date.parse('2026-10-24T10:00:00.000Z');
const item = { id: 'i1', title: 'Butter 500 g', pricePaise: 28_000 };
const batches: ExpiringBatch[] = [
  { id: 'b1', itemId: 'i1', itemName: 'Butter', expiresOn: '2026-10-26', quantity: 4 },
  { id: 'b2', itemId: 'i2', itemName: 'Curd', expiresOn: '2026-10-25', quantity: 2 },
];
const ctx = { batches, now: NOW };

describe('validateDraft', () => {
  it('builds a percent offer on items', () => {
    const v = validateDraft(emptyDraft({ items: [item], value: '20', oncePerCustomer: true, maxRedemptions: '50' }), ctx);
    expect(v.errors).toEqual({});
    expect(v.bodies).toEqual([{ kind: 'percent', value: 20, scope: 'items', storeItemIds: ['i1'], channels: 'both', perCustomerLimit: 1, maxRedemptions: 50 }]);
  });

  it('caps a percent at 90 (D-155)', () => {
    expect(validateDraft(emptyDraft({ items: [item], value: '90' }), ctx).errors.value).toBeUndefined();
    expect(validateDraft(emptyDraft({ items: [item], value: '91' }), ctx).errors.value?.code).toBe('percent');
    expect(validateDraft(emptyDraft({ items: [item], value: '0' }), ctx).errors.value?.code).toBe('percent');
    expect(validateDraft(emptyDraft({ items: [item], value: '12.5' }), ctx).errors.value?.code).toBe('percent');
  });

  it('keeps a flat offer (per unit) below the cheapest price', () => {
    const ok = validateDraft(emptyDraft({ choice: 'flat', items: [item], value: '20' }), ctx);
    expect(ok.bodies[0]).toMatchObject({ kind: 'flat', value: 2000 });
    expect(validateDraft(emptyDraft({ choice: 'flat', items: [item], value: '280' }), ctx).errors.value?.code).toBe('flatTooHigh');
  });

  it('forces the scope: a bill offer covers the whole shop, buy X get Y one item', () => {
    expect(scopeOf({ choice: 'bill_min', scope: 'items' })).toBe('store');
    expect(scopeOf({ choice: 'bxgy', scope: 'category' })).toBe('items');
    const bill = validateDraft(emptyDraft({ choice: 'bill_min', items: [item], value: '50', minBill: '500' }), ctx);
    expect(bill.bodies[0]).toEqual({ kind: 'bill_min', value: 5000, minBillPaise: 50_000, scope: 'store', channels: 'both' });
    expect(validateDraft(emptyDraft({ choice: 'bill_min', value: '600', minBill: '500' }), ctx).errors.minBill?.code).toBe('discountAboveBill');
    expect(validateDraft(emptyDraft({ choice: 'bxgy', items: [item, { ...item, id: 'i9' }], buyQty: '2', getQty: '1' }), ctx).errors.items?.code).toBe('oneItem');
    expect(validateDraft(emptyDraft({ choice: 'bxgy', items: [item], buyQty: '2', getQty: '1' }), ctx).bodies[0]).toMatchObject({ kind: 'bxgy', buyQty: 2, getQty: 1 });
  });

  it('makes one flash offer per ticked batch, with no schedule', () => {
    const v = validateDraft(emptyDraft({ choice: 'flash', batchIds: ['b1', 'b2'], value: '30', endsAt: '2026-12-01T10:00' }), ctx);
    expect(v.errors).toEqual({});
    expect(v.bodies.map((b) => [b.batchId, b.storeItemIds, b.kind, b.value, b.endsAt])).toEqual([
      ['b1', ['i1'], 'percent', 30, undefined],
      ['b2', ['i2'], 'percent', 30, undefined],
    ]);
    expect(validateDraft(emptyDraft({ choice: 'flash', batchIds: ['nope'], value: '30' }), ctx).errors.batches?.code).toBe('batchesRequired');
  });

  it('asks for items or a category', () => {
    expect(validateDraft(emptyDraft({ value: '10' }), ctx).errors.items?.code).toBe('itemsRequired');
    expect(validateDraft(emptyDraft({ scope: 'category', value: '10' }), ctx).errors.category?.code).toBe('categoryRequired');
    expect(validateDraft(emptyDraft({ scope: 'category', categoryId: 'c1', value: '10' }), ctx).bodies[0]).toMatchObject({ scope: 'category', categoryId: 'c1' });
  });

  it('checks the title length', () => {
    expect(validateDraft(emptyDraft({ items: [item], value: '10', title: 'x'.repeat(41) }), ctx).errors.title?.code).toBe('title');
  });
});

describe('checkSchedule', () => {
  it('accepts no dates and a sensible pair', () => {
    expect(checkSchedule('', '', NOW)).toEqual({ errors: {} });
    const r = checkSchedule('2026-10-25T09:00', '2026-10-27T09:00', NOW);
    expect(r.errors).toEqual({});
    expect(r.startsAt).toBeDefined();
    expect(r.endsAt).toBeDefined();
  });
  it('refuses a start in the past and an end before the start', () => {
    expect(checkSchedule('2026-10-01T09:00', '', NOW).errors.startsAt?.code).toBe('startPast');
    expect(checkSchedule('2026-10-25T09:00', '2026-10-25T08:00', NOW).errors.endsAt?.code).toBe('endBeforeStart');
    expect(checkSchedule('', '2026-10-01T08:00', NOW).errors.endsAt?.code).toBe('endBeforeStart');
  });
});
