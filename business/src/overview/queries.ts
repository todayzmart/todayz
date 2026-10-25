import { dismissStoreInsight, getStoreOverview, listStoreInsights } from '@todayz/api-client/gen/insights/insights';
import type { InsightKind } from '@todayz/api-client/gen/model/insightKind';
import type { InsightList } from '@todayz/api-client/gen/model/insightList';
import type { StoreOverview } from '@todayz/api-client/gen/model/storeOverview';
import type { StaffRole } from '@todayz/api-client/gen/model/staffRole';
import { keepPreviousData, queryOptions, skipToken, useQuery, type QueryClient } from '@tanstack/react-query';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { insightId, type InsightData, type OverviewData, type Period, type StockSource } from './types';

export const overviewKey = (sid: string) => ['overview', sid] as const;
/** Every Demand insights answer of the store: the card and the Insights tab's pages. */
export const insightsKey = (sid: string) => ['insights', sid] as const;

/** A stock movement source (D-01) as the top items table names it. */
export function stockSource(source: string): StockSource {
  if (source === 'sale_pos' || source === 'pos_reversal') return 'pos';
  if (source === 'sale_online') return 'online';
  return 'stock';
}

/** `GET {S}/overview` onto the screen's shape. The countdown uses the server's clock (`serverNow`), not the device's. */
export function toOverviewData(o: StoreOverview): OverviewData {
  const now = Date.parse(o.serverNow);
  return {
    period: o.range,
    ownerName: o.ownerName,
    storeName: o.storeName,
    now: o.serverNow,
    syncedAt: o.serverNow,
    kpis: o.kpis,
    newOrders: o.newOrders.map((n) => ({
      id: n.id,
      code: n.code,
      customer: n.customer,
      summary: n.summary,
      totalPaise: n.totalPaise,
      fulfilment: n.fulfilment,
      secondsToAccept: n.acceptBy === null ? null : Math.max(0, Math.floor((Date.parse(n.acceptBy) - now) / 1000)),
    })),
    salesByHour: o.salesByHour,
    topItems: o.topItems.map((t) => ({
      id: t.storeItemId,
      name: t.name,
      size: t.size,
      sold: t.sold,
      last7: t.last7,
      revenuePaise: t.revenuePaise,
      inStock: t.inStock,
      low: t.low,
      lastChange: t.lastChange ? { source: stockSource(t.lastChange.source), at: t.lastChange.at } : null,
    })),
    distinctItemsSold: o.distinctItemsSold,
    setup: { percent: o.setup.percent, next: o.setup.next, steps: o.setup.steps },
  };
}

export const overviewQuery = (store: CurrentStore, period: Period) =>
  queryOptions({
    queryKey: [...overviewKey(store.sid), period] as const,
    queryFn: async () => toOverviewData(await getStoreOverview(store.bid, store.sid, { range: period })),
  });

/**
 * Overview, Insights, Analytics and Customers are for owners and managers (04: `insights.view`,
 * `customers.view`); a cashier never sees them, so never meets a 403 (06 "Role limits in the UI").
 */
export const canSeeInsights = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/**
 * Today's overview for the frame (sidebar Stock badge and setup card), the same cache entry as the
 * Overview page's "Today". Nothing is asked for a cashier or before the store is known.
 */
export function useTodayOverview() {
  const current = useCurrentStore();
  const store = current.status === 'ready' && canSeeInsights(current.store.role) ? current.store : null;
  return useQuery({
    queryKey: store ? [...overviewKey(store.sid), 'today'] : ['overview', 'none', 'today'],
    queryFn: store ? async () => toOverviewData(await getStoreOverview(store.bid, store.sid, { range: 'today' })) : skipToken,
  });
}

/** Switching the period keeps the last answer on screen until the new one arrives (no skeleton flash). */
export function useOverview(store: CurrentStore, period: Period) {
  return useQuery({ ...overviewQuery(store, period), placeholderData: keepPreviousData });
}

export function withIds(list: InsightList): { period: InsightList['period']; items: InsightData[]; nextCursor: string | null } {
  return { period: list.period, items: list.items.map((i) => ({ ...i, id: insightId(i) })), nextCursor: list.nextCursor };
}

/** The card's rows: the API already keeps the top 4 by severity, one per kind (D-99). */
export function useInsightCard(store: CurrentStore) {
  return useQuery({
    queryKey: [...insightsKey(store.sid), 'card'] as const,
    queryFn: async () => withIds(await listStoreInsights(store.bid, store.sid)),
  });
}

/**
 * "Not useful" (D-99): quiets the row's subject for `coach_dismiss_quiet_days` on the server, then
 * reloads every insights answer so the next row moves up. Returns when the subject is quiet until.
 */
export async function dismissInsight(qc: QueryClient, store: CurrentStore, kind: InsightKind, subjectRef: string): Promise<string> {
  const res = await dismissStoreInsight(store.bid, store.sid, { kind, subjectRef });
  await qc.invalidateQueries({ queryKey: insightsKey(store.sid) });
  return res.quietUntil;
}
