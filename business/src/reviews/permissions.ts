import type { ShopRole } from '../app/shop';

/** Reviews are for the owner and manager (01 §2: "review replies … owner and manager"). The API is the real gate. */
export function canReviews(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}
