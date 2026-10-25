import { getBusinessStatement, listBusinessStatements } from '@todayz/api-client/gen/admin-payouts/admin-payouts';
import type { BusinessStatementList } from '@todayz/api-client/gen/model/businessStatementList';
import { useInfiniteQuery } from '@tanstack/react-query';

const PAGE = 12;

export const statementsKey = (bid: string) => ['statements', bid] as const;

/** The business's closed months, newest first, a cursor page at a time (owner and manager). */
export function useStatements(bid: string) {
  return useInfiniteQuery({
    queryKey: statementsKey(bid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listBusinessStatements(bid, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: BusinessStatementList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
    retry: false,
  });
}

/** One month's statement as CSV text, fetched with the session token by `request` (no token in a URL). */
export async function getStatementCsv(bid: string, period: string): Promise<string> {
  const body = await getBusinessStatement(bid, period, { format: 'csv' }, { headers: { Accept: 'text/csv, application/problem+json' } });
  if (typeof body !== 'string') throw new TypeError('statement: expected CSV text');
  return body;
}
