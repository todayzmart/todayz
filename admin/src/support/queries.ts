import {
  addAdminTicketNote,
  assignAdminTicket,
  closeAdminTicket,
  createAdminTicketAttachment,
  getAdminTicket,
  listAdminTickets,
  replyAdminTicket,
  setAdminTicketPriority,
} from '@todayz/api-client/gen/admin-support/admin-support';
import type { ListAdminTicketsParams } from '@todayz/api-client/gen/model/listAdminTicketsParams';
import type { TicketAttachment } from '@todayz/api-client/gen/model/ticketAttachment';
import type { TicketAttachmentContentType } from '@todayz/api-client/gen/model/ticketAttachmentContentType';
import type { TicketCloseRequest } from '@todayz/api-client/gen/model/ticketCloseRequest';
import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import type { TicketPriority } from '@todayz/api-client/gen/model/ticketPriority';
import type { TicketMessageRequest } from '@todayz/api-client/gen/model/ticketMessageRequest';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { PAGE } from '../audit/queries';

type Filter = Omit<ListAdminTicketsParams, 'limit' | 'cursor'>;

export const ticketsKey = (f: Filter) => ['admin', 'support', 'list', f] as const;
export const ticketKey = (id: string) => ['admin', 'support', 'one', id] as const;

/** The inbox: open tickets soonest-due first, the rest by last message; counts per status come with each page. */
export function useTickets(f: Filter) {
  return useInfiniteQuery({
    queryKey: ticketsKey(f),
    queryFn: ({ pageParam }) => listAdminTickets({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useTicket(id: string) {
  return useQuery({ queryKey: ticketKey(id), queryFn: () => getAdminTicket(id) });
}

function settle(qc: QueryClient, id: string, d?: TicketDetail) {
  if (d) qc.setQueryData(ticketKey(id), d);
  void qc.invalidateQueries({ queryKey: ['admin', 'support', 'list'] });
}

/** Assign to an agent or, with null, unassign (audited); assigning the current assignee changes nothing. */
export function useAssign(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (assigneeId: string | null) => assignAdminTicket(id, { assigneeId }),
    onSuccess: (d) => settle(qc, id, d),
  });
}

/**
 * Change the priority (audited, D-226). An open ticket's due time becomes its timer's start plus the
 * new priority's minutes, so it may turn overdue at once; the same priority changes nothing.
 */
export function usePriority(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (priority: TicketPriority) => setAdminTicketPriority(id, { priority }),
    onSuccess: (d) => settle(qc, id, d),
  });
}

/**
 * A reply the opener sees, or an internal note they never do. Each sends an Idempotency-Key, so a
 * retry after a dropped connection never posts the message twice.
 */
export function useMessage(id: string, kind: 'reply' | 'note') {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (body: TicketMessageRequest) =>
      once((headers) => (kind === 'reply' ? replyAdminTicket : addAdminTicketNote)(id, body, { headers })),
    onSuccess: (d) => settle(qc, id, d),
  });
}

/** Resolve (the opener can still reopen it) or close (final), with a reason; audited. */
export function useClose(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: TicketCloseRequest) => closeAdminTicket(id, body),
    onSuccess: (d) => settle(qc, id, d),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['admin', 'audit'] }),
  });
}

/** What the composer accepts (the contract's TicketAttachmentContentType); 10 MB at most. */
export const ACCEPT: readonly TicketAttachmentContentType[] = ['image/jpeg', 'image/png', 'image/webp', 'application/pdf'];
export const MAX_BYTES = 10 * 1024 * 1024;
export const MAX_FILES = 5;

export class UploadError extends Error {}

/**
 * Asks for a presigned PUT, then sends the file straight to storage (MinIO locally, R2 deployed).
 * The returned attachment is the caller's until a message claims it by id.
 */
export function useUpload(id: string) {
  return useMutation({
    mutationFn: async (file: File): Promise<TicketAttachment> => {
      const made = await createAdminTicketAttachment(id, {
        fileName: file.name.slice(0, 120),
        contentType: file.type as TicketAttachmentContentType,
        sizeBytes: file.size,
      });
      const res = await fetch(made.upload.url, {
        method: made.upload.method,
        headers: { 'Content-Type': made.upload.headers['Content-Type'] },
        body: file,
      });
      if (!res.ok) throw new UploadError(String(res.status));
      return made.attachment;
    },
  });
}
