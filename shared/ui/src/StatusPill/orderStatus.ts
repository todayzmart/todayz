export type PillTone =
  | 'neutral'
  | 'pending'
  | 'accepted'
  | 'preparing'
  | 'ready'
  | 'delivery'
  | 'completed'
  | 'rejected'
  | 'cancelled'
  | 'paid';

/** Every order status in the data model (03-data-model §4). */
export type OrderStatus =
  | 'placed'
  | 'awaiting_customer'
  | 'accepted'
  | 'ready'
  | 'out_for_delivery'
  | 'collected'
  | 'delivered'
  | 'rejected'
  | 'expired'
  | 'cancelled'
  | 'delivery_failed';

const ORDER_TONE: Record<OrderStatus, PillTone> = {
  placed: 'pending',
  awaiting_customer: 'pending',
  accepted: 'accepted',
  ready: 'ready',
  out_for_delivery: 'delivery',
  collected: 'completed',
  delivered: 'completed',
  rejected: 'rejected',
  delivery_failed: 'rejected',
  expired: 'cancelled',
  cancelled: 'cancelled',
};

/** The pill tone for an order status. The label itself comes from the app's i18n catalogue. */
export function orderStatusTone(status: OrderStatus): PillTone {
  return ORDER_TONE[status];
}
