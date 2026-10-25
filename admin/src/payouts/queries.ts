import {
  exportPayouts,
  getAdminPayout,
  holdPayout,
  listAdminPayouts,
  listAdminStatements,
  markPayoutPaid,
  unholdPayout,
} from '@todayz/api-client/gen/admin-payouts/admin-payouts';
import type { AdminPayoutDetail } from '@todayz/api-client/gen/model/adminPayoutDetail';
import type { PayoutStatus } from '@todayz/api-client/gen/model/payoutStatus';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { PAGE } from '../audit/queries';

export const statementsKey = (period: string) => ['admin', 'payouts', 'statements', period] as const;
export const payoutsKey = (period: string, status?: PayoutStatus) => ['admin', 'payouts', 'list', { period, status }] as const;
export const payoutKey = (id: string) => ['admin', 'payouts', 'one', id] as const;

/** One month's statements, one per business, paged on the server's cursor. */
export function useStatements(period: string) {
  return useInfiniteQuery({
    queryKey: statementsKey(period),
    queryFn: ({ pageParam }) => listAdminStatements({ period, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function usePayouts(period: string, status?: PayoutStatus) {
  return useInfiniteQuery({
    queryKey: payoutsKey(period, status),
    queryFn: ({ pageParam }) =>
      listAdminPayouts({ period, ...(status ? { status } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function usePayout(id: string) {
  return useQuery({ queryKey: payoutKey(id), queryFn: () => getAdminPayout(id) });
}

export type PayoutChange =
  | { kind: 'hold' | 'unhold'; reason: string }
  /** `amountPaise` is the amount on screen: the server refuses it if a close recomputed the payout since. */
  | { kind: 'paid'; utr: string; amountPaise: number; reason: string };

function refresh(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ['admin', 'payouts'] });
  void qc.invalidateQueries({ queryKey: ['admin', 'audit'] });
}

/**
 * Hold, unhold or mark paid (finance and super admin, step-up). Each sends an Idempotency-Key, so a
 * retry after a dropped connection gets the first answer and mark paid never posts twice.
 */
export function usePayoutChange(id: string) {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (c: PayoutChange): Promise<AdminPayoutDetail> =>
      once((headers) => {
        if (c.kind === 'paid') return markPayoutPaid(id, { utr: c.utr, amountPaise: c.amountPaise, reason: c.reason }, { headers });
        return (c.kind === 'hold' ? holdPayout : unholdPayout)(id, { reason: c.reason }, { headers });
      }),
    onSuccess: (d) => qc.setQueryData(payoutKey(id), d),
    onSettled: () => refresh(qc),
  });
}

/** The bank/UPI batch CSV of the month's payable payouts (step-up). It holds bank details: saved, never shown. */
export function useExportPayouts() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (period: string) => exportPayouts({ period }, { headers: { Accept: 'text/csv' } }),
    onSettled: () => refresh(qc),
  });
}

/** Hands a CSV to the browser as a file download. */
export function saveCsv(name: string, csv: string) {
  if (typeof URL.createObjectURL !== 'function') return;
  const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
