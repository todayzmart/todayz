import { request } from '@todayz/api-client';

// TODO(W1-04): replace these local types and the call below with the generated client once
// `GET /v1/biz/businesses` is in the contract. The shape here is the planned one, not a given.
export type ShopRole = 'owner' | 'manager' | 'cashier';
export interface ShopStore {
  id: string;
  name: string;
}
export interface ShopBusiness {
  id: string;
  name: string;
  role: ShopRole;
  stores: ShopStore[];
}

export function listBusinesses(): Promise<{ items: ShopBusiness[] }> {
  return request<{ items: ShopBusiness[] }>('/v1/biz/businesses');
}

const KEY = 'todayz.biz.store';

/** The store last used on this device (B-01 "remembers last store"). */
export function rememberedStore(): string | null {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
}

export function rememberStore(id: string): void {
  try {
    localStorage.setItem(KEY, id);
  } catch {
    // Private mode: the picker simply asks again next time.
  }
}
