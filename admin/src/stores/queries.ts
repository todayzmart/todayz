import {
  forceCloseAdminStore,
  getAdminStore,
  listAdminStores,
  reopenAdminStore,
  resetAdminStoreStrikes,
  reviewAdminStore,
  updateAdminStore,
} from '@todayz/api-client/gen/admin-stores/admin-stores';
import type { AdminStore } from '@todayz/api-client/gen/model/adminStore';
import type { AdminStoreHours } from '@todayz/api-client/gen/model/adminStoreHours';
import type { AdminStorePin } from '@todayz/api-client/gen/model/adminStorePin';
import type { AdminStoreStatus } from '@todayz/api-client/gen/model/adminStoreStatus';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

export const storeKey = (id: string) => ['admin', 'store', id] as const;

export function useStores(filter: { status?: AdminStoreStatus; city?: string }) {
  return useInfiniteQuery({
    queryKey: ['admin', 'stores', filter],
    queryFn: ({ pageParam }) => listAdminStores({ ...filter, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useStore(id: string) {
  return useQuery({ queryKey: storeKey(id), queryFn: () => getAdminStore(id) });
}

/** The A-05 changes; each answers the store as it is now and is audited on it. */
export type StoreChange =
  | { kind: 'edit'; reason: string; location?: AdminStorePin; hours?: AdminStoreHours }
  | { kind: 'forceClose'; reason: string; until: string }
  | { kind: 'reopen' | 'resetStrikes'; reason: string }
  | { kind: 'review'; decision: 'approve' | 'send_back'; note: string };

function send(id: string, c: StoreChange): Promise<AdminStore> {
  switch (c.kind) {
    case 'edit':
      return updateAdminStore(id, {
        reason: c.reason,
        ...(c.location ? { location: c.location } : {}),
        ...(c.hours ? { hours: c.hours } : {}),
      });
    case 'forceClose':
      return forceCloseAdminStore(id, { reason: c.reason, until: c.until });
    case 'reopen':
      return reopenAdminStore(id, { reason: c.reason });
    case 'resetStrikes':
      return resetAdminStoreStrikes(id, { reason: c.reason });
    case 'review':
      return reviewAdminStore(id, { decision: c.decision, ...(c.note ? { note: c.note } : {}) });
  }
}

/** Not idempotency-keyed: each change checks the state it starts from, so a repeat is a 409. */
export function useStoreChange(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: StoreChange) => send(id, c),
    onSuccess: (s) => qc.setQueryData(storeKey(id), s),
    onSettled: (s) => {
      void qc.invalidateQueries({ queryKey: ['admin', 'stores'] });
      if (s) void qc.invalidateQueries({ queryKey: ['admin', 'business', s.businessId] });
      refreshAudit(qc, 'store', id);
    },
  });
}
