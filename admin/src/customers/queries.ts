import {
  blockAdminCustomer,
  getAdminCustomer,
  resetAdminCustomerStrikes,
  revokeAdminCustomerSession,
  unblockAdminCustomer,
} from '@todayz/api-client/gen/admin-customers/admin-customers';
import type { AdminCustomer } from '@todayz/api-client/gen/model/adminCustomer';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { refreshAudit } from '../audit/queries';

export const customerKey = (id: string) => ['admin', 'customer', id] as const;

export function useCustomer(id: string) {
  return useQuery({ queryKey: customerKey(id), queryFn: () => getAdminCustomer(id) });
}

export type CustomerChange = 'block' | 'unblock' | 'reset';

/** Block, unblock or reset strikes, each with a reason; the answer is the customer as it is now. */
export function useCustomerChange(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ kind, reason }: { kind: CustomerChange; reason: string }): Promise<AdminCustomer> =>
      kind === 'block'
        ? blockAdminCustomer(id, { reason })
        : kind === 'unblock'
          ? unblockAdminCustomer(id, { reason })
          : resetAdminCustomerStrikes(id, { reason }),
    onSuccess: (c) => qc.setQueryData(customerKey(id), c),
    onSettled: () => refreshAudit(qc, 'user', id),
  });
}

/** Signs the customer out of one device. */
export function useRevokeSession(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sessionId: string) => revokeAdminCustomerSession(id, sessionId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: customerKey(id) });
      refreshAudit(qc, 'user', id);
    },
  });
}
