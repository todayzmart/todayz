import { getAdminPulse, getAdminPulseMap } from '@todayz/api-client/gen/admin-pulse/admin-pulse';
import type { AdminPulseRange } from '@todayz/api-client/gen/model/adminPulseRange';
import { keepPreviousData, useQuery } from '@tanstack/react-query';

/** Today's figures refresh every 5 minutes on the server (`rollup_today`); the page follows at the same pace. */
const REFRESH_MS = 5 * 60_000;

/**
 * `enabled` is false for a role without Pulse: on `/` the shell redirects it to its first screen,
 * but during that redirect the index page is still mounted for a moment and must not call the API.
 */
export function usePulse(range: AdminPulseRange, enabled: boolean) {
  return useQuery({
    enabled,
    queryKey: ['admin', 'pulse', range],
    queryFn: () => getAdminPulse({ range }),
    placeholderData: keepPreviousData,
    refetchInterval: REFRESH_MS,
  });
}

export function usePulseMap(range: AdminPulseRange, enabled: boolean) {
  return useQuery({
    enabled,
    queryKey: ['admin', 'pulse-map', range],
    queryFn: () => getAdminPulseMap({ range }),
    placeholderData: keepPreviousData,
  });
}
