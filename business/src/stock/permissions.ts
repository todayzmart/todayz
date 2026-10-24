import type { ShopRole } from '../app/shop';

/**
 * What the Stock screens show per role (D-06, D-38). The API is the real gate; this only hides what
 * a role cannot do, so a person does not meet a 403 in a normal flow.
 *
 * - cashier: see items and count stock (`item.view`, `stock.count`)
 * - manager and owner: also restock, damage and expiry, price edits, listing, batches, adding items
 *   and bulk import (`stock.restock`, `stock.damage`, `item.price.edit`, `item.manage`,
 *   `batches.manage`)
 */
export type StockAction = 'count' | 'restock' | 'damage' | 'price' | 'manage' | 'batches' | 'addItem' | 'import';

export function canStock(role: ShopRole, action: StockAction): boolean {
  if (action === 'count') return true;
  return role === 'owner' || role === 'manager';
}
