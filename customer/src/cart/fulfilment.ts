// C-09 / C-11 pickup or delivery (01 §5.5, D-94). The person picks the way for this shop's cart and,
// for delivery, one of their saved addresses. Delivery is offered only when the shop delivers and
// the address is inside its radius; the radius is checked here so the switch can say so early, and
// the server checks it again on every quote and on placing (the answer that counts). Nothing about
// the address is kept here but its id.
import type { Address } from '@todayz/api-client/gen/model/address';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import { getDiscoverStore } from '@todayz/api-client/gen/discover/discover';
import { useQuery } from '@tanstack/react-query';
import { useSyncExternalStore } from 'react';
import { useAddresses } from '../address/queries';
import type { CartState } from './cart';

export type Mode = 'pickup' | 'delivery';

interface Choice {
  storeId: string;
  mode: Mode;
  addressId: string | null;
}

let choice: Choice | null = null;
const listeners = new Set<() => void>();

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

function set(next: Choice | null): void {
  choice = next;
  listeners.forEach((l) => l());
}

/** The way chosen for this shop's cart: pickup unless the person chose delivery. */
export function useFulfilmentChoice(storeId: string | undefined): { mode: Mode; addressId: string | null } {
  const c = useSyncExternalStore(subscribe, () => choice, () => choice);
  return c && storeId !== undefined && c.storeId === storeId ? { mode: c.mode, addressId: c.addressId } : { mode: 'pickup', addressId: null };
}

export function setFulfilment(storeId: string, mode: Mode, addressId: string | null = null): void {
  set({ storeId, mode, addressId });
}

export function clearFulfilment(): void {
  set(null);
}

/** Tests only. */
export function resetFulfilmentForTests(): void {
  set(null);
}

const EARTH_M = 6_371_000;

/** Great-circle distance in metres. */
export function distanceM(a: { lat: number; lng: number }, b: { lat: number; lng: number }): number {
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLng = (b.lng - a.lng) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Whether the shop delivers to this address (inclusive of the radius, as the server's ST_DWithin). */
export function inRadius(store: Pick<DiscoverStorePage, 'lat' | 'lng' | 'deliveryRadiusM'>, a: Pick<Address, 'lat' | 'lng'>): boolean {
  return store.deliveryRadiusM > 0 && distanceM(store, a) <= store.deliveryRadiusM;
}

/** What is sent on the quote and on placing. */
export type Fulfilment = { fulfilment: 'pickup' } | { fulfilment: 'delivery'; addressId: string };

/** The fulfilment part of the quote's cache key. */
export function fulfilmentKey(f: Fulfilment): string {
  return f.fulfilment === 'delivery' ? `delivery:${f.addressId}` : 'pickup';
}

/**
 * The shop's delivery terms, the person's addresses and what the cart is quoted for. Delivery is
 * quoted only with an address inside the radius: the chosen one, else the default, else the
 * first that fits. Without one the cart stays priced for pickup and `needsAddress` says why.
 */
export function useFulfilment(cart: CartState, signedIn: boolean) {
  const storeId = cart.store?.id;
  const slug = cart.store?.slug ?? '';
  const picked = useFulfilmentChoice(storeId);
  const store = useQuery({
    queryKey: ['store-page', slug],
    enabled: slug !== '',
    queryFn: () => getDiscoverStore(slug),
    staleTime: 60_000,
  });
  const offered = store.data?.delivery === true;
  const addresses = useAddresses(signedIn && offered);
  const list = addresses.data?.addresses ?? [];
  const fits = store.data ? list.filter((a) => inRadius(store.data, a)) : [];
  const wantsDelivery = offered && picked.mode === 'delivery';
  const address = wantsDelivery
    ? (fits.find((a) => a.addressId === picked.addressId) ?? fits.find((a) => a.isDefault) ?? fits[0] ?? null)
    : null;
  const quoted: Fulfilment = address ? { fulfilment: 'delivery', addressId: address.addressId } : { fulfilment: 'pickup' };
  return {
    store: store.data,
    offered,
    mode: wantsDelivery ? ('delivery' as const) : ('pickup' as const),
    addresses: list,
    addressesLoading: addresses.isPending && addresses.fetchStatus !== 'idle',
    address,
    quoted,
    /** Delivery chosen but no saved address is inside the radius yet. */
    needsAddress: wantsDelivery && !addresses.isPending && address === null,
    choose: (mode: Mode, addressId: string | null = picked.addressId) => {
      if (storeId) setFulfilment(storeId, mode, addressId);
    },
  };
}
