import type { Store } from '@todayz/api-client/gen/model/store';
import type { StoreCreate } from '@todayz/api-client/gen/model/storeCreate';
import type { StoreUpdate } from '@todayz/api-client/gen/model/storeUpdate';

/** Form state for a store's name, address, pin and phone. Everything is text until it is sent. */
export interface StoreValues {
  name: string;
  addressLine1: string;
  addressLine2: string;
  landmark: string;
  locality: string;
  city: string;
  pincode: string;
  /** The ten digits after +91. */
  phone: string;
  lat: string;
  lng: string;
}

export const EMPTY_STORE: StoreValues = {
  name: '',
  addressLine1: '',
  addressLine2: '',
  landmark: '',
  locality: '',
  city: '',
  pincode: '',
  phone: '',
  lat: '',
  lng: '',
};

export function valuesOf(store: Store | undefined): StoreValues {
  if (!store) return EMPTY_STORE;
  return {
    name: store.name,
    addressLine1: store.addressLine1 ?? '',
    addressLine2: store.addressLine2 ?? '',
    landmark: store.landmark ?? '',
    locality: store.locality ?? '',
    city: store.city ?? '',
    pincode: store.pincode ?? '',
    phone: store.phone?.replace(/^\+91/, '') ?? '',
    lat: store.location ? String(store.location.lat) : '',
    lng: store.location ? String(store.location.lng) : '',
  };
}

export type StoreFieldErrors = Partial<Record<keyof StoreValues, string>>;

const PIN = /^[1-9][0-9]{5}$/;
const MOBILE = /^[6-9][0-9]{9}$/;

/** Error keys (`stores.form.err.<key>`) per field; empty when the values can be sent. */
export function validateStore(v: StoreValues): StoreFieldErrors {
  const e: StoreFieldErrors = {};
  if (v.name.trim() === '') e.name = 'required';
  if (v.pincode.trim() !== '' && !PIN.test(v.pincode.trim())) e.pincode = 'pincode';
  if (v.phone.trim() !== '' && !MOBILE.test(v.phone.trim())) e.phone = 'phone';
  const hasLat = v.lat.trim() !== '';
  const hasLng = v.lng.trim() !== '';
  if (hasLat !== hasLng) e[hasLat ? 'lng' : 'lat'] = 'pinBoth';
  if (hasLat && hasLng) {
    const lat = Number(v.lat);
    const lng = Number(v.lng);
    if (!Number.isFinite(lat) || lat < 6 || lat > 38) e.lat = 'pinRange';
    if (!Number.isFinite(lng) || lng < 68 || lng > 98) e.lng = 'pinRange';
  }
  return e;
}

const TEXT_FIELDS = ['addressLine1', 'addressLine2', 'landmark', 'locality', 'city'] as const;

/** A new store: only what was filled in. */
export function createPayload(v: StoreValues): StoreCreate {
  const out: StoreCreate = { name: v.name.trim() };
  for (const k of TEXT_FIELDS) {
    const t = v[k].trim();
    if (t) out[k] = t;
  }
  if (v.pincode.trim()) out.pincode = v.pincode.trim();
  if (v.phone.trim()) out.phone = `+91${v.phone.trim()}`;
  if (v.lat.trim() && v.lng.trim()) out.location = { lat: Number(v.lat), lng: Number(v.lng) };
  return out;
}

/** An edit: only the fields that changed; a field emptied becomes `null` (clears it). */
export function updatePayload(v: StoreValues, before: StoreValues): StoreUpdate {
  const out: StoreUpdate = {};
  if (v.name.trim() !== before.name) out.name = v.name.trim();
  for (const k of TEXT_FIELDS) {
    const t = v[k].trim();
    if (t !== before[k]) out[k] = t === '' ? null : t;
  }
  if (v.pincode.trim() !== before.pincode) out.pincode = v.pincode.trim() === '' ? null : v.pincode.trim();
  if (v.phone.trim() !== before.phone) out.phone = v.phone.trim() === '' ? null : `+91${v.phone.trim()}`;
  if (v.lat.trim() !== before.lat || v.lng.trim() !== before.lng) {
    out.location = v.lat.trim() && v.lng.trim() ? { lat: Number(v.lat), lng: Number(v.lng) } : null;
  }
  return out;
}
