import { describe, expect, it } from 'vitest';
import { EMPTY_STORE, createPayload, updatePayload, validateStore, valuesOf } from './storeValues';

describe('store form', () => {
  it('needs a name, and checks pincode, phone and the pin', () => {
    expect(validateStore(EMPTY_STORE)).toEqual({ name: 'required' });
    expect(validateStore({ ...EMPTY_STORE, name: 'A', pincode: '12', phone: '12345', lat: '12.9' })).toEqual({
      pincode: 'pincode',
      phone: 'phone',
      lng: 'pinBoth',
    });
    expect(validateStore({ ...EMPTY_STORE, name: 'A', lat: '51', lng: '77' }).lat).toBe('pinRange');
  });

  it('creates with only what was filled, phone with +91', () => {
    expect(createPayload({ ...EMPTY_STORE, name: ' Indiranagar ', city: 'Bengaluru', phone: '9000000001', lat: '12.97', lng: '77.64' })).toEqual({
      name: 'Indiranagar',
      city: 'Bengaluru',
      phone: '+919000000001',
      location: { lat: 12.97, lng: 77.64 },
    });
  });

  it('updates only what changed and clears an emptied field with null', () => {
    const before = valuesOf({
      id: 's1',
      businessId: 'b1',
      slug: 'x',
      shortCode: 'ABC123',
      name: 'Old',
      addressLine1: '12 Main Rd',
      addressLine2: null,
      landmark: 'Near the metro',
      locality: null,
      city: 'Bengaluru',
      pincode: '560038',
      location: { lat: 12.97, lng: 77.64 },
      phone: '+919000000001',
      hours: { weekly: {}, holidays: [] },
      status: 'draft',
      openNow: false,
      createdAt: '2026-10-20T00:00:00Z',
    });
    expect(updatePayload({ ...before, name: 'New', landmark: '', lat: '', lng: '' }, before)).toEqual({
      name: 'New',
      landmark: null,
      location: null,
    });
    expect(updatePayload(before, before)).toEqual({});
  });
});
