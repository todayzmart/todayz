import type { ShopRole } from '../app/shop';

/** The coach is for the owner and manager (01 §2). The API is the real gate. */
export function canCoach(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}
