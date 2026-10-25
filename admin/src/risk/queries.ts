import { blockAdminCustomer } from '@todayz/api-client/gen/admin-customers/admin-customers';
import { suspendAdminBusiness } from '@todayz/api-client/gen/admin-businesses/admin-businesses';
import { clearRiskFlag, confirmRiskFlag, getRiskFlag, listRiskFlags } from '@todayz/api-client/gen/admin-risk/admin-risk';
import type { RiskFlagStatus } from '@todayz/api-client/gen/model/riskFlagStatus';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

export const flagsKey = (status: RiskFlagStatus, rule: string | undefined) => ['admin', 'risk', 'flags', status, rule ?? ''] as const;
export const flagKey = (id: string) => ['admin', 'risk', 'flag', id] as const;

export function useFlags(status: RiskFlagStatus, rule: string | undefined) {
  return useInfiniteQuery({
    queryKey: flagsKey(status, rule),
    queryFn: ({ pageParam }) => listRiskFlags({ status, ...(rule ? { rule } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** One flag with its facts, evidence, subject and the subject's other flags. */
export function useFlag(id: string) {
  return useQuery({ queryKey: flagKey(id), queryFn: () => getRiskFlag(id) });
}

function refresh(qc: QueryClient, id: string) {
  void qc.invalidateQueries({ queryKey: ['admin', 'risk'] });
  refreshAudit(qc, 'risk_flag', id);
}

export type Verdict = 'clear' | 'confirm';

/**
 * Clear or confirm with a reason (D-225). Not idempotency-keyed: a resolved flag answers
 * `409 invalid_transition`, so a retry can never resolve it twice. Neither moves money or blocks.
 */
export function useResolve(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ verdict, reason }: { verdict: Verdict; reason: string }) =>
      verdict === 'clear' ? clearRiskFlag(id, { reason }) : confirmRiskFlag(id, { reason }),
    onSettled: () => refresh(qc, id),
  });
}

export type FollowUp = 'block' | 'suspend';

/**
 * After a confirm, the customer's block (A-06) or the business's suspend (A-03): their own calls,
 * audited on the customer or the business, never part of the confirm.
 */
export function useFollowUp(flagId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ kind, id, reason }: { kind: FollowUp; id: string; reason: string }): Promise<void> => {
      if (kind === 'block') await blockAdminCustomer(id, { reason });
      else await suspendAdminBusiness(id, { reason });
    },
    onSettled: (_d, _e, v) => {
      void qc.invalidateQueries({ queryKey: flagKey(flagId) });
      void qc.invalidateQueries({ queryKey: ['admin', v.kind === 'block' ? 'customers' : 'businesses'] });
      refreshAudit(qc, v.kind === 'block' ? 'user' : 'business', v.id);
    },
  });
}
