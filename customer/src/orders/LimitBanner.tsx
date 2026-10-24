// C-21 restricted state (BK-16): the banner on Home and Checkout while no-show strikes limit the account.
import { getMe } from '@todayz/api-client/gen/me/me';
import { useSessionState } from '@todayz/auth';
import { useQuery } from '@tanstack/react-query';
import { Banner } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { formatUntil } from './limit';

/** The C-21 banner while the limit lasts. Signed-out people and accounts without a limit see nothing. */
export function LimitBanner() {
  const intl = useIntl();
  const { status } = useSessionState();
  const me = useQuery({ queryKey: ['me'], queryFn: () => getMe(), enabled: status === 'signedIn' });
  const limit = me.data?.customer?.activeOrderLimit;
  // The server ends the limit on its own clock; this only hides a banner already past its date when
  // `/v1/me` was read (every refetch re-checks it).
  if (!limit || Date.parse(limit.until) <= me.dataUpdatedAt) return null;
  return (
    <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'orders.limit.title' })}>
      {intl.formatMessage(
        { id: 'orders.limit.body' },
        { max: limit.maxActiveOrders, date: formatUntil(intl, limit.until) },
      )}
    </Banner>
  );
}
