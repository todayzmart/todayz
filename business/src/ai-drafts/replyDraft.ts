import { confirmReviewReplyDraft, createReviewReplyDraft, getReviewReplyDraft } from '@todayz/api-client/gen/ai/ai';
import type { ReviewReplyDecision } from '@todayz/api-client/gen/model/reviewReplyDecision';
import type { ReviewReplyDraft } from '@todayz/api-client/gen/model/reviewReplyDraft';
import type { StoreReview } from '@todayz/api-client/gen/model/storeReview';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import type { CurrentStore } from '../orders/store';
import { reviewsKey } from '../reviews/queries';
import { useAction } from '../stores/queries';
import { useAiFeatures, useDraft, withKey } from './queries';

/** The confirm for a drafted reply: the words as drafted are `accepted`, anything else `edited` with the person's text. */
export function replyDecision(lineId: string, drafted: string, clean: string): ReviewReplyDecision {
  return clean === drafted.trim() ? { lineId, outcome: 'accepted' } : { lineId, outcome: 'edited', value: { text: clean } };
}

/**
 * AI-R6 for one review: offered for a first reply only. The drafted words fill the reply box; the
 * reply is posted only when the person sends it, through the draft's confirm (the server posts
 * the reply there), so a draft never reaches the customer on its own.
 */
export function useReplyDraft(store: CurrentStore, review: StoreReview, fill: (text: string) => void) {
  const features = useAiFeatures(store);
  const qc = useQueryClient();
  const action = useAction();
  const [draftId, setDraftId] = useState<string | null>(null);
  const create = useAction();
  const draft = useDraft<ReviewReplyDraft>(store, 'review-reply', draftId, (id) => getReviewReplyDraft(store.bid, store.sid, id));
  const d = draft.data;
  const line = d?.status === 'ready' ? (d.lines[0] ?? null) : null;
  const filled = useRef<string | null>(null);
  const fillRef = useRef(fill);
  useEffect(() => {
    fillRef.current = fill;
  });

  useEffect(() => {
    if (d && line && filled.current !== d.draftId) {
      filled.current = d.draftId;
      fillRef.current(line.text);
    }
  }, [d, line]);

  const start = async () => {
    // Keyed per review: a retry after a dropped connection gets the same draft, not a second call.
    const made = await create.run(`create:${review.id}`, (key) => createReviewReplyDraft(store.bid, store.sid, { reviewId: review.id }, withKey(key)));
    if (made) setDraftId(made.draftId);
  };

  /** Sends the reply through the draft. False when it did not go (the error is in `sendError`). */
  const send = async (clean: string): Promise<boolean> => {
    if (!d || !line) return false;
    const decision = replyDecision(line.lineId, line.text, clean);
    const out = await action.run('confirm', (key) => confirmReviewReplyDraft(store.bid, store.sid, d.draftId, { lines: [decision] }, withKey(key)));
    if (!out) return false;
    void qc.invalidateQueries({ queryKey: reviewsKey(store.sid) });
    return true;
  };

  return {
    available: features.reviewReply && review.reply === null,
    start,
    busy: create.pending || d?.status === 'processing',
    drafted: line !== null,
    failed: d?.status === 'failed',
    error: create.error ?? (draft.isError ? draft.error : null),
    send,
    sending: action.pending,
    sendError: action.error,
  };
}
