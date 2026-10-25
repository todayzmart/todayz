import type { AdminBannerState } from '@todayz/api-client/gen/model/adminBannerState';
import { text, ulid } from '../app/search';

export const STATES = ['review', 'live', 'removed'] as const satisfies readonly AdminBannerState[];

/** A-20 view, kept in the URL: the queue, a store filter, and the banner open beside the list. */
export interface BannersSearch {
  /** Absent is `review`: the uploads waiting for a person come first. */
  state?: Exclude<AdminBannerState, 'review'>;
  storeId?: string;
  /** The banner shown in the side panel. */
  banner?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateBannersSearch(raw: Record<string, unknown>): BannersSearch {
  const out: BannersSearch = {};
  const s = text(raw.state);
  if (s === 'live' || s === 'removed') out.state = s;
  for (const k of ['storeId', 'banner'] as const) {
    const id = ulid(raw[k]);
    if (id) out[k] = id;
  }
  return out;
}
