import { keepPreviousData, queryOptions, useQuery } from '@tanstack/react-query';
import { sampleOverview } from './sample';
import type { OverviewData, Period } from './types';

/**
 * The Overview's one data source. Today it answers from `sample.ts` with no network; W3-03
 * replaces `fetchOverview` with the API call (and maps the response onto `OverviewData`), and
 * nothing else changes.
 */
export async function fetchOverview(period: Period): Promise<OverviewData> {
  return Promise.resolve(sampleOverview(period));
}

export const overviewQuery = (period: Period) =>
  queryOptions({
    queryKey: ['overview', period] as const,
    queryFn: () => fetchOverview(period),
  });

/** Switching the period keeps the last answer on screen until the new one arrives (no skeleton flash). */
export function useOverview(period: Period) {
  return useQuery({ ...overviewQuery(period), placeholderData: keepPreviousData });
}
