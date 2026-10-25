import type { AdminAuditEntry } from '@todayz/api-client/gen/model/adminAuditEntry';
import { useCallback } from 'react';
import { useIntl } from 'react-intl';

/** Audit actions the console names; any other action shows as written (it is still audited). */
export function useActionLabel() {
  const intl = useIntl();
  return useCallback(
    (action: string): string => {
      switch (action) {
        case 'customer.block':
          return intl.formatMessage({ id: 'audit.action.customerBlock' });
        case 'customer.unblock':
          return intl.formatMessage({ id: 'audit.action.customerUnblock' });
        case 'customer.strikes_reset':
          return intl.formatMessage({ id: 'audit.action.strikesReset' });
        case 'customer.session_revoke':
          return intl.formatMessage({ id: 'audit.action.sessionRevoke' });
        case 'order.admin_cancel':
          return intl.formatMessage({ id: 'audit.action.orderCancel' });
        default:
          return action;
      }
    },
    [intl],
  );
}

/** Who did it: the admin's name when the log has it, else the kind of actor. */
export function useActorLabel() {
  const intl = useIntl();
  return useCallback(
    (e: Pick<AdminAuditEntry, 'actorKind' | 'actorName'>): string => {
      if (e.actorName) return e.actorName;
      switch (e.actorKind) {
        case 'admin':
          return intl.formatMessage({ id: 'audit.actor.admin' });
        case 'staff':
          return intl.formatMessage({ id: 'audit.actor.staff' });
        case 'user':
          return intl.formatMessage({ id: 'audit.actor.user' });
        case 'device':
          return intl.formatMessage({ id: 'audit.actor.device' });
        default:
          return intl.formatMessage({ id: 'audit.actor.system' });
      }
    },
    [intl],
  );
}
