import type { ShopRole } from '../app/shop';

/** Credits, referrals and the standee are for owner and manager (01 §3). The API is the real gate. */
export function canSeeCredits(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}
