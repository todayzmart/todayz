import type { PosSale } from '@todayz/api-client/gen/model/posSale';
import type { SyncChange } from '@todayz/api-client/gen/model/syncChange';
import { describe, expect, it } from 'vitest';
import { e164, needsOnline, saleToWire, toAck, toCatalogueChanges, toReverseAck, toSyncEntry } from './api';
import type { Sale } from './types';

function sale(over: Partial<Sale> = {}): Sale {
  return {
    clientId: 's1',
    deviceId: 'dev',
    deviceSeq: 4,
    soldAt: '2026-10-24T10:00:00.000Z',
    lines: [
      { clientId: 'l1', storeItemId: 'item-1', name: 'Rice 1 kg', unitPricePaise: 5000, quantity: 2, discountPaise: 500, lineTotalPaise: 9500 },
      { clientId: 'l2', storeItemId: null, name: 'Loose sugar', unitPricePaise: 4000, quantity: 1, discountPaise: 0, lineTotalPaise: 4000 },
    ],
    subtotalPaise: 13500,
    billDiscountPaise: 300,
    roundOffPaise: 0,
    creditsUsed: 0,
    totalPaise: 13200,
    paymentMethod: 'upi',
    link: null,
    ...over,
  };
}

describe('sale to the online request', () => {
  it('sends lines as typed, omits a custom line id and zero discounts, and never an expected total', () => {
    const w = saleToWire(sale());
    expect(w).toEqual({
      clientId: 's1',
      lines: [
        { clientId: 'l1', storeItemId: 'item-1', name: 'Rice 1 kg', unitPricePaise: 5000, quantity: 2, manualDiscountPaise: 500 },
        { clientId: 'l2', name: 'Loose sugar', unitPricePaise: 4000, quantity: 1 },
      ],
      billDiscountPaise: 300,
      paymentMethod: 'upi',
    });
    expect('expectedTotalPaise' in w).toBe(false);
  });

  it('maps each link kind, with the phone as +91 E.164', () => {
    expect(saleToWire(sale({ link: { method: 'bill_qr', claimId: 'c1', creditsUsed: 5 } })).link).toEqual({ method: 'bill_qr', claimId: 'c1' });
    expect(saleToWire(sale({ link: { method: 'counter_code', phone: '9000000001', code: '4821', codeAt: 'x', clockOffsetMs: 1 } })).link).toEqual({
      method: 'counter_code',
      phone: '+919000000001',
    });
    expect(saleToWire(sale({ link: { method: 'phone_unverified', phone: '9000000001' } })).link).toEqual({ method: 'phone_unverified', phone: '+919000000001' });
  });

  it('knows which sales cannot wait for a sync', () => {
    expect(needsOnline(sale({ link: { method: 'bill_qr', claimId: 'c1', creditsUsed: 0 } }))).toBe(true);
    expect(needsOnline(sale({ link: { method: 'phone_unverified', phone: '9000000001' } }))).toBe(false);
    expect(needsOnline(sale())).toBe(false);
  });

  it('keeps a number that already has a country code', () => {
    expect(e164('9000000001')).toBe('+919000000001');
    expect(e164('+919000000001')).toBe('+919000000001');
  });
});

describe('sale to a sync entry', () => {
  it('carries the device time, the sequence and the counter code with its entry time and clock offset', () => {
    const s = sale({ link: { method: 'counter_code', phone: '9000000001', code: '4821', codeAt: '2026-10-24T09:59:50.000Z', clockOffsetMs: 1200.4 } });
    const e = toSyncEntry({ clientId: 's1', seq: 4, kind: 'sale', at: 'ignored', payload: s });
    expect(e).toMatchObject({
      clientId: 's1',
      deviceSeq: 4,
      kind: 'sale',
      at: '2026-10-24T10:00:00.000Z',
      sale: {
        paymentMethod: 'upi',
        billDiscountPaise: 300,
        link: { method: 'counter_code', phone: '+919000000001', code: '4821', codeEnteredAt: '2026-10-24T09:59:50.000Z', clockOffsetMs: 1200 },
      },
    });
  });

  it('degrades a counter code that was already dropped to an unverified phone', () => {
    const s = sale({ link: { method: 'counter_code', phone: '9000000001', code: '', codeAt: 'x', clockOffsetMs: null } });
    expect(toSyncEntry({ clientId: 's1', seq: 4, kind: 'sale', at: 'x', payload: s }).sale?.link).toEqual({ method: 'phone_unverified', phone: '+919000000001' });
  });

  it('maps stock movements: a count is "counted", the rest are quantities', () => {
    const at = '2026-10-24T10:00:00.000Z';
    expect(toSyncEntry({ clientId: 'm1', seq: 5, kind: 'movement', at, payload: { kind: 'count_correction', storeItemId: 'i', quantity: 6 } })).toMatchObject({
      kind: 'count',
      stock: { storeItemId: 'i', counted: 6 },
    });
    expect(toSyncEntry({ clientId: 'm2', seq: 6, kind: 'movement', at, payload: { kind: 'damage', storeItemId: 'i', quantity: 2, note: 'wet' } })).toMatchObject({
      kind: 'damage',
      stock: { storeItemId: 'i', quantity: 2, note: 'wet' },
    });
  });
});

const posSale: PosSale = {
  id: 'sale-1',
  clientId: 's1',
  lines: [
    { id: 'a', clientId: 'l1', storeItemId: 'item-1', name: 'Rice', unitPricePaise: 5000, quantity: 2, offerDiscountPaise: 200, manualDiscountPaise: 500, lineTotalPaise: 9300 },
    { id: 'b', clientId: 'l2', storeItemId: null, name: 'Sugar', unitPricePaise: 4000, quantity: 1, offerDiscountPaise: 0, manualDiscountPaise: 0, lineTotalPaise: 4000 },
  ],
  itemSubtotalPaise: 13300,
  billOfferDiscountPaise: 100,
  subtotalPaise: 13200,
  discountPaise: 800,
  roundOffPaise: -20,
  creditsUsed: 5,
  cashbackCredits: 3,
  totalPaise: 12000,
  paymentMethod: 'cash',
  link: { method: 'bill_qr', status: 'linked', customerName: 'Asha' },
  status: 'completed',
  reversedAt: null,
  reversalReason: null,
  soldAt: '2026-10-24T10:00:00Z',
  receivedAt: '2026-10-24T10:00:01Z',
  receiptToken: 'EXAMPLE_receipt_000000',
  version: 1,
  serverNow: '2026-10-24T10:00:01Z',
};

describe('the server sale shown as returned', () => {
  it('adds offer and manual discounts per line and separates the bill discount', () => {
    const a = toAck(posSale);
    expect(a.lines).toEqual([
      { clientId: 'l1', unitPricePaise: 5000, discountPaise: 700, lineTotalPaise: 9300 },
      { clientId: 'l2', unitPricePaise: 4000, discountPaise: 0, lineTotalPaise: 4000 },
    ]);
    // discountPaise 800 = 500 manual line + 300 manual bill; plus the bill offer of 100.
    expect(a.billDiscountPaise).toBe(400);
    expect(a).toMatchObject({ saleId: 'sale-1', totalPaise: 12000, roundOffPaise: -20, creditsUsed: 5, cashbackCredits: 3, linkStatus: 'linked', receiptToken: 'EXAMPLE_receipt_000000' });
  });

  it('lists what a reversal gives back from the returned sale', () => {
    expect(toReverseAck(posSale)).toEqual({ stockReturned: [{ storeItemId: 'item-1', quantity: 2 }], creditsReturned: 5, cashbackClawedBack: 3 });
  });
});

describe('catalogue changes', () => {
  const item = { id: 'i1', productId: 'p1', name: 'Rice', pricePaise: 5000, mrpPaise: 5500, onHand: 4, reserved: 1, available: 3, isListed: true, countNeeded: false, version: 2 };
  it('keeps store item upserts and deletes and ignores products, categories and offers', () => {
    const changes: SyncChange[] = [
      { seq: 1, entity: 'store_item', entityId: 'i1', op: 'upsert', item },
      { seq: 2, entity: 'store_item', entityId: 'i2', op: 'delete', item: null },
      { seq: 3, entity: 'offer', entityId: 'o1', op: 'upsert', item: null },
      { seq: 4, entity: 'category', entityId: 'c1', op: 'delete', item: null },
    ];
    expect(toCatalogueChanges(changes)).toEqual([
      { seq: 1, entityId: 'i1', op: 'upsert', item: { id: 'i1', name: 'Rice', pricePaise: 5000, available: 3, isListed: true } },
      { seq: 2, entityId: 'i2', op: 'delete' },
    ]);
  });
});
