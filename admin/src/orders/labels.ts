import type { AdminCancelReason } from '@todayz/api-client/gen/model/adminCancelReason';
import type { AdminOrderEventActorKind } from '@todayz/api-client/gen/model/adminOrderEventActorKind';
import type { AdminOrderStatus } from '@todayz/api-client/gen/model/adminOrderStatus';
import type { AdminStrikeSide } from '@todayz/api-client/gen/model/adminStrikeSide';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';

/** Statuses an order cannot leave (03 §4.1); admin cancel is offered from every other one. */
export const TERMINAL: ReadonlySet<string> = new Set(['collected', 'delivered', 'rejected', 'expired', 'cancelled', 'delivery_failed']);

export const STATUSES: readonly AdminOrderStatus[] = [
  'placed',
  'awaiting_customer',
  'accepted',
  'ready',
  'out_for_delivery',
  'collected',
  'delivered',
  'rejected',
  'expired',
  'cancelled',
  'delivery_failed',
];

export const CANCEL_REASONS: readonly AdminCancelReason[] = ['customer_request', 'store_request', 'fraud_suspected', 'technical_issue', 'other'];
export const STRIKE_SIDES: readonly AdminStrikeSide[] = ['none', 'customer', 'store'];

export function useOrderLabels() {
  const intl = useIntl();
  const status = useCallback(
    (s: string): string => {
      switch (s) {
        case 'placed':
          return intl.formatMessage({ id: 'orders.status.placed' });
        case 'awaiting_customer':
          return intl.formatMessage({ id: 'orders.status.awaitingCustomer' });
        case 'accepted':
          return intl.formatMessage({ id: 'orders.status.accepted' });
        case 'ready':
          return intl.formatMessage({ id: 'orders.status.ready' });
        case 'out_for_delivery':
          return intl.formatMessage({ id: 'orders.status.outForDelivery' });
        case 'collected':
          return intl.formatMessage({ id: 'orders.status.collected' });
        case 'delivered':
          return intl.formatMessage({ id: 'orders.status.delivered' });
        case 'rejected':
          return intl.formatMessage({ id: 'orders.status.rejected' });
        case 'expired':
          return intl.formatMessage({ id: 'orders.status.expired' });
        case 'cancelled':
          return intl.formatMessage({ id: 'orders.status.cancelled' });
        case 'delivery_failed':
          return intl.formatMessage({ id: 'orders.status.deliveryFailed' });
        default:
          return s;
      }
    },
    [intl],
  );
  const fulfilment = useCallback(
    (f: string) => (f === 'delivery' ? intl.formatMessage({ id: 'orders.fulfilment.delivery' }) : intl.formatMessage({ id: 'orders.fulfilment.pickup' })),
    [intl],
  );
  const actor = useCallback(
    (a: AdminOrderEventActorKind) => {
      switch (a) {
        case 'customer':
          return intl.formatMessage({ id: 'orders.actor.customer' });
        case 'staff':
          return intl.formatMessage({ id: 'orders.actor.staff' });
        case 'admin':
          return intl.formatMessage({ id: 'orders.actor.admin' });
        default:
          return intl.formatMessage({ id: 'orders.actor.system' });
      }
    },
    [intl],
  );
  const reason = useCallback(
    (r: AdminCancelReason) => {
      switch (r) {
        case 'customer_request':
          return intl.formatMessage({ id: 'orders.cancel.reason.customerRequest' });
        case 'store_request':
          return intl.formatMessage({ id: 'orders.cancel.reason.storeRequest' });
        case 'fraud_suspected':
          return intl.formatMessage({ id: 'orders.cancel.reason.fraudSuspected' });
        case 'technical_issue':
          return intl.formatMessage({ id: 'orders.cancel.reason.technicalIssue' });
        default:
          return intl.formatMessage({ id: 'orders.cancel.reason.other' });
      }
    },
    [intl],
  );
  const side = useCallback(
    (s: AdminStrikeSide) => {
      switch (s) {
        case 'customer':
          return intl.formatMessage({ id: 'orders.cancel.strike.customer' });
        case 'store':
          return intl.formatMessage({ id: 'orders.cancel.strike.store' });
        default:
          return intl.formatMessage({ id: 'orders.cancel.strike.none' });
      }
    },
    [intl],
  );
  return useMemo(() => ({ status, fulfilment, actor, reason, side }), [status, fulfilment, actor, reason, side]);
}
