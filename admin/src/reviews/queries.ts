import {
  hideReview,
  hideReviewReply,
  listAdminReviewReports,
  listAdminReviews,
  resolveReviewReport,
  restoreReview,
  restoreReviewReply,
} from '@todayz/api-client/gen/admin-reviews/admin-reviews';
import type { ListAdminReviewReportsStatus } from '@todayz/api-client/gen/model/listAdminReviewReportsStatus';
import type { ListAdminReviewsParams } from '@todayz/api-client/gen/model/listAdminReviewsParams';
import type { ResolveReportRequest } from '@todayz/api-client/gen/model/resolveReportRequest';
import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

type ReviewFilter = Omit<ListAdminReviewsParams, 'limit' | 'cursor'>;

export const reviewsKey = (f: ReviewFilter) => ['admin', 'reviews', f] as const;
export const reportsKey = (status: ListAdminReviewReportsStatus) => ['admin', 'review-reports', status] as const;

export function useReviews(f: ReviewFilter) {
  return useInfiniteQuery({
    queryKey: reviewsKey(f),
    queryFn: ({ pageParam }) => listAdminReviews({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useReports(status: ListAdminReviewReportsStatus) {
  return useInfiniteQuery({
    queryKey: reportsKey(status),
    queryFn: ({ pageParam }) => listAdminReviewReports({ status, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** The four moderation moves (01 §10, D-41); each needs a reason and is audited on the review. */
export const MOVES = {
  hide: hideReview,
  restore: restoreReview,
  replyHide: hideReviewReply,
  replyRestore: restoreReviewReply,
} as const;
export type Move = keyof typeof MOVES;

function refreshLists(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ['admin', 'reviews'] });
  void qc.invalidateQueries({ queryKey: ['admin', 'review-reports'] });
}

/**
 * Hide or restore a review or its reply. Not idempotency-keyed: a repeat of the same move is
 * `409 invalid_transition`, so a retry can never apply it twice.
 */
export function useModerate(reviewId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ move, reason }: { move: Move; reason: string }) => MOVES[move](reviewId, { reason }),
    onSettled: () => {
      refreshLists(qc);
      refreshAudit(qc, 'review', reviewId);
    },
  });
}

/** Close a report as dismissed or upheld; a resolved report answers `409 invalid_transition`. */
export function useResolveReport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: ResolveReportRequest }) => resolveReviewReport(id, body),
    onSettled: () => refreshLists(qc),
  });
}
