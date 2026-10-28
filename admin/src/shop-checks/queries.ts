import { checkAdminShop, listAdminShopChecks, rejectAdminShop } from '@todayz/api-client/gen/admin-businesses/admin-businesses';
import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';
import { businessKey } from '../businesses/queries';

const QUEUE = ['admin', 'shop-checks'] as const;

/** The live shops nobody has checked yet, newest first (D-274). */
export function useShopChecks() {
  return useInfiniteQuery({
    queryKey: QUEUE,
    queryFn: ({ pageParam }) => listAdminShopChecks({ limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** A check or a reject takes the shop off the queue and changes what A-02 and A-03 show. */
function settle(qc: QueryClient, id: string) {
  void qc.invalidateQueries({ queryKey: QUEUE });
  void qc.invalidateQueries({ queryKey: businessKey(id) });
  void qc.invalidateQueries({ queryKey: ['admin', 'businesses'] });
  void qc.invalidateQueries({ queryKey: ['admin', 'stores'] });
  refreshAudit(qc, 'business', id);
}

export function useCheckShop() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, note }: { id: string; note: string }) => checkAdminShop(id, note ? { note } : {}),
    onSuccess: (_r, { id }) => settle(qc, id),
  });
}

/** Hides the shop and cancels its open online orders; the answer says how many. */
export function useRejectShop() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) => rejectAdminShop(id, { reason }),
    onSuccess: (_r, { id }) => {
      settle(qc, id);
      void qc.invalidateQueries({ queryKey: ['admin', 'orders'] });
    },
  });
}
