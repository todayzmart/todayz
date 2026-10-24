import type { ShopRole } from '../app/shop';

/** Offers and banners: owner and manager only (01 §4, D-06). The API is the real gate; a cashier sees "Ask a manager". */
export function canManageOffers(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}
