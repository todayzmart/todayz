import { ApiError } from '@todayz/api-client';
import { listMyOrders } from '@todayz/api-client/gen/orders/orders';
import {
  createMyTicket,
  createMyTicketAttachment,
  getMyTicketThread,
  listMyTickets,
  postMyTicketMessage,
} from '@todayz/api-client/gen/support/support';
import type { MyTicketAttachment } from '@todayz/api-client/gen/model/myTicketAttachment';
import type { MyTicketAttachmentContentType } from '@todayz/api-client/gen/model/myTicketAttachmentContentType';
import type { MyTicketCreate } from '@todayz/api-client/gen/model/myTicketCreate';
import type { MyTicketList } from '@todayz/api-client/gen/model/myTicketList';
import type { MyTicketMessageCreate } from '@todayz/api-client/gen/model/myTicketMessageCreate';
import type { MyTicketThread } from '@todayz/api-client/gen/model/myTicketThread';
import { useInfiniteQuery, useQuery, type QueryClient } from '@tanstack/react-query';
import { PAGE_SIZE, RECENT_ORDERS } from './limits';

export const ticketsKey = ['support'] as const;
export const ticketListKey = [...ticketsKey, 'list'] as const;
export const threadKey = (ticketId: string) => [...ticketsKey, 'thread', ticketId] as const;

/** The customer's own tickets, newest activity first; `fetchNextPage` loads more. */
export function useTickets() {
  return useInfiniteQuery({
    queryKey: ticketListKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listMyTickets({ limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: MyTicketList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

/** One ticket and its messages, oldest first. A 404 (not theirs) is not retried. */
export function useThread(ticketId: string) {
  return useQuery({
    queryKey: threadKey(ticketId),
    queryFn: () => getMyTicketThread(ticketId),
    retry: (n, err) => !(err instanceof ApiError && err.status < 500) && n < 2,
  });
}

/** The customer's recent orders for the "Which order?" picker. */
export function useRecentOrders() {
  return useQuery({
    queryKey: ['orders', 'help-picker'],
    queryFn: async () => (await listMyOrders({ limit: RECENT_ORDERS })).items,
  });
}

/** Opens the ticket; the same key on a retry after a lost answer opens it once. */
export async function openTicket(qc: QueryClient, body: MyTicketCreate, idempotencyKey: string): Promise<MyTicketThread> {
  const thread = await createMyTicket(body, { idempotencyKey });
  qc.setQueryData(threadKey(thread.ticket.id), thread);
  void qc.invalidateQueries({ queryKey: ticketListKey });
  return thread;
}

/** Adds the customer's message; a closed ticket answers 409 and the thread is reloaded. */
export async function sendMessage(qc: QueryClient, ticketId: string, body: MyTicketMessageCreate, idempotencyKey: string): Promise<MyTicketThread> {
  try {
    const thread = await postMyTicketMessage(ticketId, body, { idempotencyKey });
    qc.setQueryData(threadKey(ticketId), thread);
    void qc.invalidateQueries({ queryKey: ticketListKey });
    return thread;
  } catch (err) {
    if (err instanceof ApiError && (err.status === 409 || err.status === 404)) void qc.invalidateQueries({ queryKey: threadKey(ticketId) });
    throw err;
  }
}

/**
 * Registers the file, then sends it straight to storage on the presigned PUT. Only Content-Type is
 * set: the browser adds Content-Length itself, and the signature covers both.
 */
export async function uploadFile(file: File): Promise<MyTicketAttachment> {
  const made = await createMyTicketAttachment({
    fileName: file.name.slice(0, 120),
    contentType: file.type as MyTicketAttachmentContentType,
    sizeBytes: file.size,
  });
  const res = await fetch(made.upload.url, { method: made.upload.method, headers: { 'Content-Type': made.upload.headers['Content-Type'] }, body: file });
  if (!res.ok) throw new Error(`upload ${res.status}`);
  return made.attachment;
}

/** The message id for a failed call; unknown codes are `help.error.generic`. */
export function helpErrorId(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'validation_failed':
      case 'not_found':
      case 'invalid_transition':
        return `help.error.${err.code}`;
      case 'rate_limited':
        return 'auth.error.rateLimited';
    }
  }
  return 'help.error.generic';
}
