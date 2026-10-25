import { cancelAdminOrder, getAdminOrder, listAdminOrders } from '@todayz/api-client/gen/admin-orders/admin-orders';
import type { AdminCancelOrderRequest } from '@todayz/api-client/gen/model/adminCancelOrderRequest';
import type { ListAdminOrdersParams } from '@todayz/api-client/gen/model/listAdminOrdersParams';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { PAGE, refreshAudit } from '../audit/queries';

export const ordersKey = (f: Omit<ListAdminOrdersParams, 'limit' | 'cursor'>) => ['admin', 'orders', f] as const;
export const orderKey = (id: string) => ['admin', 'order', id] as const;

export function useOrders(f: Omit<ListAdminOrdersParams, 'limit' | 'cursor'>) {
  return useInfiniteQuery({
    queryKey: ordersKey(f),
    queryFn: ({ pageParam }) => listAdminOrders({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useOrder(id: string) {
  return useQuery({ queryKey: orderKey(id), queryFn: () => getAdminOrder(id) });
}

/** Admin cancel (D-20, D-60), idempotent: a retry after a dropped connection cannot cancel twice. */
export function useCancelOrder(id: string) {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (body: AdminCancelOrderRequest) => once((headers) => cancelAdminOrder(id, body, { headers })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: orderKey(id) });
      void qc.invalidateQueries({ queryKey: ['admin', 'orders'] });
      refreshAudit(qc, 'order', id);
    },
  });
}
