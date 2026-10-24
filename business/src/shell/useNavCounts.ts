import { listStoreOrders } from '@todayz/api-client/gen/orders/orders';
import { skipToken, useQuery } from '@tanstack/react-query';
import { useIntl } from 'react-intl';
import { listKey } from '../orders/queries';
import { useCurrentStore } from '../orders/store';
import { useOverview } from '../overview/queries';
import { SAMPLE_DATA } from '../overview/sample';
import type { NavCount } from './NavLink';

/**
 * Sidebar badges (design: Orders and Stock carry a neutral count).
 *
 * Orders counts the live New list: the same query and cache entry as the Orders page's New tab,
 * so the store stream's invalidation keeps it fresh. Stock comes from the Overview's query for
 * today, and stays hidden while the Overview runs on sample data (a made-up number next to real
 * orders would mislead). No data yet, or an error: no badge.
 */
export function useNavCounts(): Partial<Record<string, NavCount>> {
  const intl = useIntl();
  const current = useCurrentStore();
  const store = current.status === 'ready' ? current.store : null;
  const list = useQuery({
    queryKey: store ? listKey(store.sid, 'new') : ['orders', 'none', 'list', 'new'],
    queryFn: store ? () => listStoreOrders(store.bid, store.sid, { status: 'new', limit: 50 }) : skipToken,
  });
  const { data } = useOverview('today');
  const counts: Partial<Record<string, NavCount>> = {};
  if (list.data) {
    const n = list.data.items.length;
    counts.orders = { count: n, label: intl.formatMessage({ id: 'shell.nav.ordersCount' }, { count: n }) };
  }
  if (!SAMPLE_DATA && data) {
    const n = data.kpis.sellOutSoon;
    counts.stock = { count: n, label: intl.formatMessage({ id: 'shell.nav.stockCount' }, { count: n }) };
  }
  return counts;
}
