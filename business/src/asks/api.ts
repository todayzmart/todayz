// B-20 data: the open asks (also the Asks tab count), the done list, and the reply and report calls.
// Live over the store stream (`ask.created`, `ask.closed` refetch everything under asksKey), with a
// slow poll while the tab is open.
import { ApiError } from '@todayz/api-client';
import { listStoreAsks, replyToAsk, reportAsk } from '@todayz/api-client/gen/asks/asks';
import type { AskReplyCreate } from '@todayz/api-client/gen/model/askReplyCreate';
import type { AskReportCreateReason } from '@todayz/api-client/gen/model/askReportCreateReason';
import type { StoreAsk } from '@todayz/api-client/gen/model/storeAsk';
import type { StoreAskList } from '@todayz/api-client/gen/model/storeAskList';
import { newUlid } from '@todayz/auth';
import { useInfiniteQuery, useQuery, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';

export const asksKey = (sid: string) => ['asks', sid] as const;
const openKey = (sid: string) => ['asks', sid, 'open'] as const;
const doneKey = (sid: string) => ['asks', sid, 'done'] as const;

/** At most this many open asks show; the API's limit is 100 and a shop rarely has more than a few. */
const OPEN_LIMIT = 100;
const DONE_PAGE = 50;

export function useOpenAsks(store: CurrentStore, live = false) {
  return useQuery<StoreAskList>({
    queryKey: openKey(store.sid),
    queryFn: () => listStoreAsks(store.bid, store.sid, { status: 'open', limit: OPEN_LIMIT }),
    refetchInterval: live ? 30_000 : false,
  });
}

/** Replied and missed asks, newest first, a page at a time (`all` less the open ones). */
export function useDoneAsks(store: CurrentStore) {
  return useInfiniteQuery({
    queryKey: doneKey(store.sid),
    queryFn: ({ pageParam }) =>
      listStoreAsks(store.bid, store.sid, { status: 'all', limit: DONE_PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor,
  });
}

/** Puts a replied or reported ask into the done list at once, then refetches both lists. */
export function storedAsk(qc: QueryClient, sid: string, ask: StoreAsk): void {
  qc.setQueryData<StoreAskList>(openKey(sid), (old) =>
    old ? { ...old, asks: old.asks.filter((a) => a.askId !== ask.askId), openCount: Math.max(0, old.openCount - (ask.status === 'open' ? 0 : 1)) } : old,
  );
  void qc.invalidateQueries({ queryKey: asksKey(sid) });
}

/**
 * One reply per ask. The Idempotency-Key stays with the ask while the sheet retries the same answer,
 * so a lost response cannot send a second reply.
 */
const replyKeys = new Map<string, string>();

export async function sendReply(store: CurrentStore, askId: string, body: AskReplyCreate): Promise<StoreAsk> {
  const slot = `${askId}:${body.answer}`;
  const key = replyKeys.get(slot) ?? newUlid();
  replyKeys.set(slot, key);
  const ask = await replyToAsk(store.bid, store.sid, askId, body, { headers: { 'Idempotency-Key': key } });
  replyKeys.delete(slot);
  return ask;
}

export function sendReport(store: CurrentStore, askId: string, reason: AskReportCreateReason): Promise<void> {
  return reportAsk(store.bid, store.sid, askId, { reason });
}

/** The i18n key suffix for a reply or report failure; unknown codes are `generic`. */
export function askErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'price_above_mrp':
      case 'ask_closed':
      case 'invalid_transition':
      case 'forbidden':
      case 'validation_failed':
        return err.code;
    }
  }
  return 'generic';
}
