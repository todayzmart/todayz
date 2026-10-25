import { listAdminAuditLog, listAdminSubjectAudit } from '@todayz/api-client/gen/admin-audit/admin-audit';
import type { AdminAuditSubjectType } from '@todayz/api-client/gen/model/adminAuditSubjectType';
import type { ListAdminAuditLogParams } from '@todayz/api-client/gen/model/listAdminAuditLogParams';
import { useInfiniteQuery, type QueryClient } from '@tanstack/react-query';

export const PAGE = 20;

export const subjectAuditKey = (type: AdminAuditSubjectType, id: string) => ['admin', 'audit', 'subject', type, id] as const;
export const auditLogKey = (f: Omit<ListAdminAuditLogParams, 'limit' | 'cursor'>) => ['admin', 'audit', 'log', f] as const;

/** One subject's history, newest first, paged on the server's cursor. */
export function useSubjectAudit(type: AdminAuditSubjectType, id: string) {
  return useInfiniteQuery({
    queryKey: subjectAuditKey(type, id),
    queryFn: ({ pageParam }) =>
      listAdminSubjectAudit({ subjectType: type, subjectId: id, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useAuditLog(f: Omit<ListAdminAuditLogParams, 'limit' | 'cursor'>) {
  return useInfiniteQuery({
    queryKey: auditLogKey(f),
    queryFn: ({ pageParam }) => listAdminAuditLog({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** After a change, the subject's panel and the log show the new row. */
export function refreshAudit(qc: QueryClient, type: AdminAuditSubjectType, id: string) {
  void qc.invalidateQueries({ queryKey: subjectAuditKey(type, id) });
  void qc.invalidateQueries({ queryKey: ['admin', 'audit', 'log'] });
}
