import { dismissCoachCard, listCoachCards, markCoachCardDone } from '@todayz/api-client/gen/coach/coach';
import type { CoachCard } from '@todayz/api-client/gen/model/coachCard';
import type { CoachCardFilter } from '@todayz/api-client/gen/model/coachCardFilter';
import type { CoachCardList } from '@todayz/api-client/gen/model/coachCardList';
import { useInfiniteQuery, useQuery, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';

/** B-19 page size. */
export const PAGE_SIZE = 20;
/** Overview shows the 1–3 newest live cards (06 B-19). */
export const PREVIEW_CARDS = 3;

export const coachKey = (s: CurrentStore) => ['biz', s.bid, 'store', s.sid, 'coach'] as const;

/** The feed for one tab, newest first; `fetchNextPage` loads more. */
export function useCoachFeed(store: CurrentStore, status: CoachCardFilter) {
  return useInfiniteQuery({
    queryKey: [...coachKey(store), 'feed', status],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      listCoachCards(store.bid, store.sid, { status, limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: CoachCardList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

/** The newest live cards for the Overview slot. */
export function useCoachPreview(store: CurrentStore) {
  return useQuery({
    queryKey: [...coachKey(store), 'preview'],
    queryFn: () => listCoachCards(store.bid, store.sid, { status: 'live', limit: PREVIEW_CARDS }),
  });
}

export type CoachAct = 'done' | 'dismiss';

/**
 * Done or Not useful on a live card. Both are safe to repeat (the server answers the card again),
 * so they carry no Idempotency-Key. Every coach list reloads afterwards, so the card moves tabs.
 */
export async function actOnCard(qc: QueryClient, s: CurrentStore, card: CoachCard, act: CoachAct): Promise<CoachCard> {
  try {
    return act === 'done' ? await markCoachCardDone(s.bid, s.sid, card.id) : await dismissCoachCard(s.bid, s.sid, card.id);
  } finally {
    void qc.invalidateQueries({ queryKey: coachKey(s) });
  }
}
