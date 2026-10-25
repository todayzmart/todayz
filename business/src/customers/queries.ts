import { listStoreCustomers } from '@todayz/api-client/gen/insights/insights';
import type { CustomerSegment } from '@todayz/api-client/gen/model/customerSegment';
import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';

/** Rows per page of B-09. */
export const CUSTOMERS_PAGE = 50;

export const customersKey = (sid: string, segment: CustomerSegment) => ['customers', sid, segment] as const;

/** `GET {S}/customers` for one segment, a page at a time on the cursor. */
export function useCustomers(store: CurrentStore, segment: CustomerSegment) {
  return useInfiniteQuery({
    queryKey: customersKey(store.sid, segment),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      listStoreCustomers(store.bid, store.sid, {
        segment,
        limit: CUSTOMERS_PAGE,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    placeholderData: keepPreviousData,
  });
}
