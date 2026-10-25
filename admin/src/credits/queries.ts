import {
  approveCreditAdjustment,
  createCreditAdjustment,
  getAdminCreditExpiryPreview,
  listAdminLedgerAccounts,
  listAdminLedgerEntries,
  listCreditAdjustments,
  rejectCreditAdjustment,
} from '@todayz/api-client/gen/admin-credits/admin-credits';
import type { CreditAdjustmentCreate } from '@todayz/api-client/gen/model/creditAdjustmentCreate';
import type { CreditAdjustmentResult } from '@todayz/api-client/gen/model/creditAdjustmentResult';
import type { CreditAdjustmentStatus } from '@todayz/api-client/gen/model/creditAdjustmentStatus';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { PAGE } from '../audit/queries';

export const entriesKey = (account: string) => ['admin', 'credits', 'entries', account] as const;
export const adjustmentsKey = (status?: CreditAdjustmentStatus) => ['admin', 'credits', 'adjustments', { status }] as const;
export const expiryKey = ['admin', 'credits', 'expiry'] as const;

/** Account search. A mutation, not a query: `q` can be a phone number, which must not sit in a query key. */
export function useAccountSearch() {
  return useMutation({ mutationFn: (q: string) => listAdminLedgerAccounts({ q }) });
}

export function useEntries(account: string) {
  return useInfiniteQuery({
    queryKey: entriesKey(account),
    queryFn: ({ pageParam }) => listAdminLedgerEntries(account, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** Credits that expire over the server's default window, per IST day. */
export function useExpiry() {
  return useQuery({ queryKey: expiryKey, queryFn: () => getAdminCreditExpiryPreview() });
}

export function useAdjustments(status?: CreditAdjustmentStatus) {
  return useInfiniteQuery({
    queryKey: adjustmentsKey(status),
    queryFn: ({ pageParam }) =>
      listCreditAdjustments({ ...(status ? { status } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

function refresh(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ['admin', 'credits'] });
  void qc.invalidateQueries({ queryKey: ['admin', 'audit'] });
}

/**
 * A wallet adjustment (finance and super admin, step-up). Above the dual-approval threshold the server
 * makes a pending request instead of posting. The Idempotency-Key makes a resend answer the first result.
 */
export function useAdjust() {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (body: CreditAdjustmentCreate): Promise<CreditAdjustmentResult> => once((headers) => createCreditAdjustment(body, { headers })),
    onSettled: () => refresh(qc),
  });
}

export interface Decision {
  kind: 'approve' | 'reject';
  id: string;
  note: string;
}

/** Approve (posts the adjustment once) or reject a pending request; a requester's reject withdraws it. */
export function useDecision() {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (d: Decision): Promise<CreditAdjustmentResult> =>
      once((headers) => (d.kind === 'approve' ? approveCreditAdjustment : rejectCreditAdjustment)(d.id, { note: d.note }, { headers })),
    onSettled: () => refresh(qc),
  });
}
