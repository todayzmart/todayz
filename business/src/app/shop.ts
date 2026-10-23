export { listBusinesses } from '@todayz/api-client/gen/business/business';
export type { BusinessSummary } from '@todayz/api-client/gen/model/businessSummary';
export type { StaffRole as ShopRole } from '@todayz/api-client/gen/model/staffRole';

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
