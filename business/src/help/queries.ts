import { ApiError } from '@todayz/api-client';
import {
  createBizTicket,
  createBizTicketAttachment,
  getBizTicketThread,
  listBizTickets,
  postBizTicketMessage,
} from '@todayz/api-client/gen/biz-support/biz-support';
import { listStoreOrders } from '@todayz/api-client/gen/orders/orders';
import type { BizAttachmentContentType } from '@todayz/api-client/gen/model/bizAttachmentContentType';
import type { BizTicketAttachment } from '@todayz/api-client/gen/model/bizTicketAttachment';
import type { BizTicketCreate } from '@todayz/api-client/gen/model/bizTicketCreate';
import type { BizTicketList } from '@todayz/api-client/gen/model/bizTicketList';
import type { BizTicketMessageCreate } from '@todayz/api-client/gen/model/bizTicketMessageCreate';
import type { BizTicketThread } from '@todayz/api-client/gen/model/bizTicketThread';
import { useInfiniteQuery, useQuery, type QueryClient } from '@tanstack/react-query';
import { PAGE_SIZE, RECENT_ORDERS } from './limits';

/** Under `['biz', bid]`, so a 403 from any business call (useAction) refreshes these too. */
export const ticketsKey = (bid: string) => ['biz', bid, 'tickets'] as const;
export const ticketListKey = (bid: string) => [...ticketsKey(bid), 'list'] as const;
export const threadKey = (bid: string, ticketId: string) => [...ticketsKey(bid), 'thread', ticketId] as const;

/** The business's tickets, newest activity first; `fetchNextPage` loads more. */
export function useTickets(bid: string) {
  return useInfiniteQuery({
    queryKey: ticketListKey(bid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listBizTickets(bid, { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: BizTicketList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

/** One ticket and its messages, oldest first. A 404 (not this business's) is not retried. */
export function useThread(bid: string, ticketId: string) {
  return useQuery({
    queryKey: threadKey(bid, ticketId),
    queryFn: () => getBizTicketThread(bid, ticketId),
    retry: (n, err) => !(err instanceof ApiError && err.status < 500) && n < 2,
  });
}

/**
 * The store's recent orders for the "Which order?" picker: finished and missed ones first, since
 * those are what owners ask about, then the ones still running. Loaded only once a store is picked.
 */
export function useRecentOrders(bid: string, sid: string | null) {
  return useQuery({
    queryKey: ['biz', bid, 'help-orders', sid],
    enabled: sid !== null,
    queryFn: async () => {
      const store = sid ?? '';
      const [done, missed, accepted] = await Promise.all(
        (['done', 'missed', 'accepted'] as const).map((status) => listStoreOrders(bid, store, { status, limit: RECENT_ORDERS })),
      );
      return [...(done?.items ?? []), ...(missed?.items ?? []), ...(accepted?.items ?? [])]
        .sort((a, b) => Date.parse(b.placedAt) - Date.parse(a.placedAt))
        .slice(0, RECENT_ORDERS);
    },
  });
}

/** Opens the ticket; the same key on a retry after a lost answer opens it once. */
export async function openTicket(qc: QueryClient, bid: string, body: BizTicketCreate, idempotencyKey: string): Promise<BizTicketThread> {
  const thread = await createBizTicket(bid, body, { idempotencyKey });
  qc.setQueryData(threadKey(bid, thread.ticket.id), thread);
  void qc.invalidateQueries({ queryKey: ticketListKey(bid) });
  return thread;
}

/** Adds the business's message; a closed ticket answers 409 and the thread is reloaded. */
export async function sendMessage(
  qc: QueryClient,
  bid: string,
  ticketId: string,
  body: BizTicketMessageCreate,
  idempotencyKey: string,
): Promise<BizTicketThread> {
  try {
    const thread = await postBizTicketMessage(bid, ticketId, body, { idempotencyKey });
    qc.setQueryData(threadKey(bid, ticketId), thread);
    void qc.invalidateQueries({ queryKey: ticketListKey(bid) });
    return thread;
  } catch (err) {
    if (err instanceof ApiError && (err.status === 409 || err.status === 404)) void qc.invalidateQueries({ queryKey: threadKey(bid, ticketId) });
    throw err;
  }
}

/**
 * Registers the file, then sends it straight to storage on the presigned PUT. Only Content-Type is
 * set: the browser adds Content-Length itself, and the signature covers both.
 */
export async function uploadFile(bid: string, file: File): Promise<BizTicketAttachment> {
  const made = await createBizTicketAttachment(bid, {
    fileName: file.name.slice(0, 120),
    contentType: file.type as BizAttachmentContentType,
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
      case 'forbidden':
      case 'not_found':
      case 'invalid_transition':
        return `help.error.${err.code}`;
      case 'rate_limited':
        return 'auth.error.rateLimited';
    }
  }
  return 'help.error.generic';
}
