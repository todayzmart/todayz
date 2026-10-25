import {
  getAdminBusiness,
  listAdminBusinesses,
  reinstateAdminBusiness,
  rejectAdminBusiness,
  requestAdminBusinessInfo,
  revealAdminBusinessKyc,
  reviewAdminBusinessDocument,
  reviewAdminBusinessKyc,
  suspendAdminBusiness,
  verifyAdminBusiness,
} from '@todayz/api-client/gen/admin-businesses/admin-businesses';
import { listAdminLeads, setAdminLeadStatus } from '@todayz/api-client/gen/admin-leads/admin-leads';
import type { AdminBusiness } from '@todayz/api-client/gen/model/adminBusiness';
import type { AdminBusinessStatus } from '@todayz/api-client/gen/model/adminBusinessStatus';
import type { AdminDocumentReviewRequestDecision } from '@todayz/api-client/gen/model/adminDocumentReviewRequestDecision';
import type { AdminInfoField } from '@todayz/api-client/gen/model/adminInfoField';
import type { AdminKycField } from '@todayz/api-client/gen/model/adminKycField';
import type { AdminKycReviewRequestDecision } from '@todayz/api-client/gen/model/adminKycReviewRequestDecision';
import type { AdminLeadStatus } from '@todayz/api-client/gen/model/adminLeadStatus';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

export const businessKey = (id: string) => ['admin', 'business', id] as const;

export function useBusinesses(status: AdminBusinessStatus) {
  return useInfiniteQuery({
    queryKey: ['admin', 'businesses', status],
    queryFn: ({ pageParam }) => listAdminBusinesses({ status, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useBusiness(id: string) {
  return useQuery({ queryKey: businessKey(id), queryFn: () => getAdminBusiness(id) });
}

/** The A-03 moves; each answers the business as it is now and is audited on it. */
export type BusinessMove =
  | { kind: 'verify'; note: string }
  | { kind: 'requestInfo'; fields: AdminInfoField[]; note: string }
  | { kind: 'reject' | 'suspend' | 'reinstate'; reason: string }
  | { kind: 'kyc'; decision: AdminKycReviewRequestDecision; note: string }
  | { kind: 'document'; docId: string; decision: AdminDocumentReviewRequestDecision; reason: string };

function send(id: string, m: BusinessMove): Promise<AdminBusiness> {
  const note = (n: string) => (n ? { note: n } : {});
  switch (m.kind) {
    case 'verify':
      return verifyAdminBusiness(id, note(m.note));
    case 'requestInfo':
      return requestAdminBusinessInfo(id, { fields: m.fields, note: m.note });
    case 'reject':
      return rejectAdminBusiness(id, { reason: m.reason });
    case 'suspend':
      return suspendAdminBusiness(id, { reason: m.reason });
    case 'reinstate':
      return reinstateAdminBusiness(id, { reason: m.reason });
    case 'kyc':
      return reviewAdminBusinessKyc(id, { decision: m.decision, ...note(m.note) });
    case 'document':
      return reviewAdminBusinessDocument(id, m.docId, { decision: m.decision, ...(m.reason ? { reason: m.reason } : {}) });
  }
}

/**
 * Not idempotency-keyed: each move checks the state it starts from, so a repeat is
 * `409 invalid_transition` and can never apply twice.
 */
export function useBusinessMove(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (m: BusinessMove) => send(id, m),
    onSuccess: (b) => qc.setQueryData(businessKey(id), b),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'businesses'] });
      void qc.invalidateQueries({ queryKey: ['admin', 'stores'] });
      refreshAudit(qc, 'business', id);
    },
  });
}

/**
 * Open one KYC field in full. The value lives in the caller's component state only: never in the
 * query cache, the URL or storage (the API answers `no-store`), and the reveal is audited.
 */
export function useReveal(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (field: AdminKycField) => revealAdminBusinessKyc(id, { field }),
    onSettled: () => refreshAudit(qc, 'business', id),
  });
}

export function useLeads(status: AdminLeadStatus | undefined) {
  return useInfiniteQuery({
    queryKey: ['admin', 'leads', status ?? 'all'],
    queryFn: ({ pageParam }) => listAdminLeads({ ...(status ? { status } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** Move a lead; the same status again is `409 invalid_transition`. */
export function useLeadStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status, note }: { id: string; status: AdminLeadStatus; note: string }) =>
      setAdminLeadStatus(id, { status, ...(note ? { note } : {}) }),
    onSettled: (_d, _e, v) => {
      void qc.invalidateQueries({ queryKey: ['admin', 'leads'] });
      refreshAudit(qc, 'lead', v.id);
    },
  });
}
