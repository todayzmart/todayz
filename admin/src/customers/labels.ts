import type { AdminCustomerStatus } from '@todayz/api-client/gen/model/adminCustomerStatus';
import type { AdminCustomerSessionApp } from '@todayz/api-client/gen/model/adminCustomerSessionApp';
import type { AdminStrikeReason } from '@todayz/api-client/gen/model/adminStrikeReason';
import type { PillTone } from '@todayz/ui';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';

export const statusTone = (s: AdminCustomerStatus): PillTone => (s === 'active' ? 'completed' : s === 'blocked' ? 'rejected' : 'neutral');

export function useCustomerLabels() {
  const intl = useIntl();
  const status = useCallback(
    (s: AdminCustomerStatus) => {
      switch (s) {
        case 'active':
          return intl.formatMessage({ id: 'customers.status.active' });
        case 'blocked':
          return intl.formatMessage({ id: 'customers.status.blocked' });
        default:
          return intl.formatMessage({ id: 'customers.status.deleted' });
      }
    },
    [intl],
  );
  const strike = useCallback(
    (r: AdminStrikeReason) => {
      switch (r) {
        case 'no_show':
          return intl.formatMessage({ id: 'customers.strike.noShow' });
        case 'dispatch_timeout':
          return intl.formatMessage({ id: 'customers.strike.dispatchTimeout' });
        default:
          return intl.formatMessage({ id: 'customers.strike.adminCancel' });
      }
    },
    [intl],
  );
  const app = useCallback(
    (a: AdminCustomerSessionApp) => {
      switch (a) {
        case 'customer':
          return intl.formatMessage({ id: 'customers.app.customer' });
        case 'business':
          return intl.formatMessage({ id: 'customers.app.business' });
        default:
          return intl.formatMessage({ id: 'customers.app.admin' });
      }
    },
    [intl],
  );
  return useMemo(() => ({ status, strike, app }), [status, strike, app]);
}
