import { ApiError } from '@todayz/api-client';
import {
  listStoreReviews,
  replyToReview,
  reportStoreReview,
  updateReviewReply,
} from '@todayz/api-client/gen/reviews/reviews';
import type { ReviewReply } from '@todayz/api-client/gen/model/reviewReply';
import type { StoreReview } from '@todayz/api-client/gen/model/storeReview';
import type { StoreReviewList } from '@todayz/api-client/gen/model/storeReviewList';
import { useInfiniteQuery, type InfiniteData, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';
import { PAGE_SIZE } from './limits';

export const reviewsKey = (sid: string) => ['reviews', sid] as const;
export const reviewListKey = (sid: string) => [...reviewsKey(sid), 'list'] as const;

/** The shop's reviews, newest first; `fetchNextPage` loads more. */
export function useReviews(store: CurrentStore) {
  return useInfiniteQuery({
    queryKey: reviewListKey(store.sid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listStoreReviews(store.bid, store.sid, { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: StoreReviewList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

function patchReview(qc: QueryClient, sid: string, id: string, change: (r: StoreReview) => StoreReview): void {
  qc.setQueriesData<InfiniteData<StoreReviewList>>({ queryKey: reviewListKey(sid) }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.map((r) => (r.id === id ? change(r) : r)) })) } : old,
  );
}

/**
 * Saves the reply: POST the first time, PATCH after (the contract has no If-Match on the reply).
 * A 409 means the reply exists already or can no longer be edited, so the list is reloaded.
 */
export async function saveReply(qc: QueryClient, s: CurrentStore, review: StoreReview, text: string): Promise<ReviewReply> {
  try {
    const body = { text };
    const reply = review.reply ? await updateReviewReply(s.bid, s.sid, review.id, body) : await replyToReview(s.bid, s.sid, review.id, body);
    patchReview(qc, s.sid, review.id, (r) => ({ ...r, reply }));
    return reply;
  } catch (err) {
    if (err instanceof ApiError && (err.status === 409 || err.status === 404)) void qc.invalidateQueries({ queryKey: reviewsKey(s.sid) });
    throw err;
  }
}

/** Reports the review; a second report returns the first one, so this is safe to repeat. */
export async function sendReport(qc: QueryClient, s: CurrentStore, reviewId: string, reason: string): Promise<void> {
  await reportStoreReview(s.bid, s.sid, reviewId, { reason });
  patchReview(qc, s.sid, reviewId, (r) => ({ ...r, reported: true }));
}

/** The i18n key suffix (`reviews.error.<suffix>`) for a failed call; unknown codes are `generic`. */
export function reviewsErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'validation_failed':
      case 'forbidden':
      case 'not_found':
      case 'review_reply_exists':
      case 'invalid_transition':
        return err.code;
    }
  }
  return 'generic';
}
