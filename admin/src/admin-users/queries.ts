import { inviteAdminUser, listAdminUsers, resetAdminUserTotp, updateAdminUser } from '@todayz/api-client/gen/admin-users/admin-users';
import type { AdminUserChange } from '@todayz/api-client/gen/model/adminUserChange';
import type { AdminUserInvite } from '@todayz/api-client/gen/model/adminUserInvite';
import type { AdminUserStatus } from '@todayz/api-client/gen/model/adminUserStatus';
import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

export const adminUsersKey = (status: AdminUserStatus) => ['admin', 'admin-users', status] as const;

export function useAdminUsers(status: AdminUserStatus) {
  return useInfiniteQuery({
    queryKey: adminUsersKey(status),
    queryFn: ({ pageParam }) => listAdminUsers({ status, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

function refresh(qc: QueryClient, id?: string) {
  void qc.invalidateQueries({ queryKey: ['admin', 'admin-users'] });
  if (id) refreshAudit(qc, 'admin_user', id);
}

/**
 * Not idempotency-keyed: inviting a number twice is `409 conflict`, and a change carries the
 * version it read, so a retry can never apply it twice.
 */
export function useInvite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminUserInvite) => inviteAdminUser(body),
    onSuccess: (a) => refresh(qc, a.id),
  });
}

export type AccountMove = { kind: 'change'; body: AdminUserChange } | { kind: 'reset'; reason: string };

export function useAccountMove(adminId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (m: AccountMove) => (m.kind === 'change' ? updateAdminUser(adminId, m.body) : resetAdminUserTotp(adminId, { reason: m.reason })),
    onSettled: () => refresh(qc, adminId),
  });
}
