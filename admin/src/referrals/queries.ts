import { listAdminReferrals, rejectAdminReferral } from '@todayz/api-client/gen/admin-referrals/admin-referrals';
import type { ListAdminReferralsParams } from '@todayz/api-client/gen/model/listAdminReferralsParams';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

export type ReferralFilter = Omit<ListAdminReferralsParams, 'limit' | 'cursor'>;

export const referralsKey = (f: ReferralFilter) => ['admin', 'referrals', f] as const;

export function useReferrals(f: ReferralFilter) {
  return useInfiniteQuery({
    queryKey: referralsKey(f),
    queryFn: ({ pageParam }) => listAdminReferrals({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/**
 * Reject with a reason (A-10): only before either side is paid, else `409 invalid_transition`.
 * Paid credits come back through a credits reversal, never through this.
 */
export function useReject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (reason: string) => rejectAdminReferral(id, { reason }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'referrals'] });
      void qc.invalidateQueries({ queryKey: ['admin', 'risk'] });
      refreshAudit(qc, 'referral', id);
    },
  });
}
