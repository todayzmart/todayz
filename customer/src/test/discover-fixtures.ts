// Discover API fixtures and a fetch stub for the W2-08 screen tests. Shapes follow the generated client.
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import type { DiscoverStoreCard } from '@todayz/api-client/gen/model/discoverStoreCard';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import { act } from '@testing-library/react';
import { setPlace } from '../app/place';
import { json, renderAt, type Handler } from './render';

export const NOW = '2026-10-23T10:00:00.000Z';
export const AGO_12_MIN = '2026-10-23T09:48:00.000Z';

export const STORE_A_ID = '01KE0S1W6W8ZV7T1C3A6MS0001';
export const STORE_B_ID = '01KE0S1W6W8ZV7T1C3A6MS0002';
export const SHOP_A = 'sri-lakshmi-stores';
export const SHOP_B = 'annapurna-kirana';

const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
const weekly = Object.fromEntries(DAYS.map((d) => [d, [{ open: '09:00', close: '21:00' }]]));

export function card(over: Partial<DiscoverStoreCard> = {}): DiscoverStoreCard {
  return {
    storeId: STORE_A_ID,
    storeSlug: SHOP_A,
    name: 'Sri Lakshmi Stores',
    locality: 'Indiranagar',
    distanceM: 450,
    open: true,
    pickup: true,
    delivery: true,
    bookable: true,
    rating: 4.6,
    ratingCount: 120,
    cashbackPct: 0,
    categoryTint: 'green',
    photoKey: null,
    ...over,
  };
}

export function item(over: Partial<DiscoverSearchItem> = {}): DiscoverSearchItem {
  return {
    storeItemId: '01KE0S1W6W8ZV7T1C3A6ITEM01',
    productId: '01KE0S1W6W8ZV7T1C3A6PROD01',
    name: 'Aashirvaad Atta',
    variant: null,
    brand: 'Aashirvaad',
    packSize: 5,
    unit: 'kg',
    pricePaise: 26000,
    mrpPaise: 29500,
    hasOffer: false,
    inStock: true,
    confirmedAt: AGO_12_MIN,
    imageKey: null,
    ...over,
  };
}

export function page(over: Partial<DiscoverStorePage> = {}): DiscoverStorePage {
  const c = card();
  return {
    storeId: c.storeId,
    storeSlug: c.storeSlug,
    name: c.name,
    locality: c.locality,
    addressLine1: null,
    addressLine2: null,
    landmark: null,
    city: 'Bengaluru',
    pincode: '560038',
    lat: 12.97,
    lng: 77.64,
    distanceM: c.distanceM,
    open: true,
    closedUntil: null,
    hours: { weekly, holidays: [] },
    pickup: true,
    delivery: true,
    bookable: true,
    rating: 4.6,
    ratingCount: 120,
    cashbackPct: 0,
    categoryTint: 'green',
    photoKey: null,
    categorySlug: 'grocery',
    categoryName: 'Grocery',
    deliveryFeePaise: 2000,
    deliveryFreeAbovePaise: null,
    deliveryMinOrderPaise: 10000,
    deliveryRadiusM: 3000,
    serverNow: NOW,
    ...over,
  } as DiscoverStorePage;
}

export const CATEGORIES = {
  items: [
    { id: '01KE0S1W6W8ZV7T1C3A6CAT001', slug: 'grocery', name: 'Grocery', names: {}, tint: 'green', icon: 'shopping-bag', children: [] },
    { id: '01KE0S1W6W8ZV7T1C3A6CAT002', slug: 'pharmacy', name: 'Pharmacy', names: {}, tint: 'rose', icon: 'pill', children: [] },
  ],
};

type Route = (url: URL) => unknown | Response;

/** Renders the app with the discover API answered from `routes` (path without query -> JSON body or Response). */
export function renderDiscover(path: string, routes: Record<string, Route>) {
  const handler: Handler = (call) => {
    const route = routes[call.path];
    if (!route) return undefined;
    const out = route(new URL(call.path + '?' + call.query.toString(), 'http://localhost'));
    return out instanceof Response ? out : json(200, out);
  };
  return renderAt(path, { handlers: [handler] });
}

export function pageOf<T>(items: T[]) {
  return { items, nextCursor: null, serverNow: NOW };
}

/** Puts a GPS place on the device after the app has rendered (renderAt clears storage first). */
export function atBengaluru(): void {
  act(() => setPlace({ kind: 'gps', lat: 12.97, lng: 77.64 }));
}
