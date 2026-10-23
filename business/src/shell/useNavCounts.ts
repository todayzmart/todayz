import { useIntl } from 'react-intl';
import { useOverview } from '../overview/queries';
import type { NavCount } from './NavLink';

/**
 * Sidebar badges (design: Orders and Stock carry a neutral count). They come from the Overview's
 * own query for today (same hook, same cache entry as the Overview page), so the numbers always
 * match its "Orders today" and "Low stock" tiles. No data yet, or an error: no badges.
 */
export function useNavCounts(): Partial<Record<string, NavCount>> {
  const intl = useIntl();
  const { data } = useOverview('today');
  if (!data) return {};
  const { waitingOrders, sellOutSoon } = data.kpis;
  return {
    orders: { count: waitingOrders, label: intl.formatMessage({ id: 'shell.nav.ordersCount' }, { count: waitingOrders }) },
    stock: { count: sellOutSoon, label: intl.formatMessage({ id: 'shell.nav.stockCount' }, { count: sellOutSoon }) },
  };
}
