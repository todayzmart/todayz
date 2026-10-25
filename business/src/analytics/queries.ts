import { getStoreAnalytics, listStoreInsights } from '@todayz/api-client/gen/insights/insights';
import type { AnalyticsRange } from '@todayz/api-client/gen/model/analyticsRange';
import type { InsightKind } from '@todayz/api-client/gen/model/insightKind';
import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';
import { insightsKey, withIds } from '../overview/queries';

/** Rows per page of the Insights tab. */
export const INSIGHTS_PAGE = 50;

/**
 * The Insights tab's pages (`all=1`), optionally one kind. They live under `insightsKey`, so
 * "Not useful" reloads them with the Overview card.
 */
export function useAllInsights(store: CurrentStore, kind: InsightKind | undefined) {
  return useInfiniteQuery({
    queryKey: [...insightsKey(store.sid), 'all', kind ?? 'every'] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      withIds(
        await listStoreInsights(store.bid, store.sid, {
          all: 1,
          limit: INSIGHTS_PAGE,
          ...(kind ? { kind } : {}),
          ...(pageParam ? { cursor: pageParam } : {}),
        }),
      ),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export const analyticsKey = (sid: string, range: AnalyticsRange) => ['analytics', sid, range] as const;

/** `GET {S}/analytics` for one range; switching keeps the last answer on screen. */
export function useAnalytics(store: CurrentStore, range: AnalyticsRange) {
  return useQuery({
    queryKey: analyticsKey(store.sid, range),
    queryFn: () => getStoreAnalytics(store.bid, store.sid, { range }),
    placeholderData: keepPreviousData,
  });
}
