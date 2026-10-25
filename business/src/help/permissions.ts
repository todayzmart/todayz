import type { ShopRole } from '../app/shop';

/**
 * Tickets are for the owner and manager (01 §13: "cashiers cannot"; `support.tickets`). Everyone
 * sees the guide and the FAQs. The API is the real gate (a cashier gets 403).
 */
export function canTickets(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}
