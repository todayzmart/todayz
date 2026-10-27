import { inviteAdminUser, listAdminUsers, unlockAdminSignIn, updateAdminUser } from '@todayz/api-client/gen/admin-users/admin-users';
import type { AdminSignInUnlock } from '@todayz/api-client/gen/model/adminSignInUnlock';
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

export function useAccountMove(adminId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminUserChange) => updateAdminUser(adminId, body),
    onSettled: () => refresh(qc, adminId),
  });
}

/**
 * Lift an admin's sign-in lock (D-264, †). Not idempotency-keyed: a second unlock of the same admin
 * is `409 invalid_transition` (nothing is locked any more), so a retry changes nothing twice.
 */
export function useUnlockSignIn(adminId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminSignInUnlock) => unlockAdminSignIn(adminId, body),
    onSettled: () => refresh(qc, adminId),
  });
}
