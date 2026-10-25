import type { ListAdminReviewsState } from '@todayz/api-client/gen/model/listAdminReviewsState';
import type { ListAdminReviewReportsStatus } from '@todayz/api-client/gen/model/listAdminReviewReportsStatus';
import { text, ulid } from '../app/search';

export const STATES = ['reported', 'visible', 'hidden'] as const satisfies readonly ListAdminReviewsState[];
export const REPORT_STATUSES = ['open', 'resolved'] as const satisfies readonly ListAdminReviewReportsStatus[];

/** A-12 view, kept in the URL: the tab, its filter, and the review open beside the list. */
export interface ReviewsSearch {
  /** Absent is the reviews list. */
  tab?: 'reports';
  /** Absent is `reported`: the moderation queue comes first. */
  state?: ListAdminReviewsState;
  storeId?: string;
  /** Report queue filter; absent is `open`. */
  status?: ListAdminReviewReportsStatus;
  /** The review shown in the side panel. */
  review?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateReviewsSearch(raw: Record<string, unknown>): ReviewsSearch {
  const out: ReviewsSearch = {};
  if (text(raw.tab) === 'reports') out.tab = 'reports';
  const s = text(raw.state);
  if ((STATES as readonly string[]).includes(s)) out.state = s as ListAdminReviewsState;
  const st = text(raw.status);
  if ((REPORT_STATUSES as readonly string[]).includes(st)) out.status = st as ListAdminReviewReportsStatus;
  for (const k of ['storeId', 'review'] as const) {
    const id = ulid(raw[k]);
    if (id) out[k] = id;
  }
  return out;
}
