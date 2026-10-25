import { getAdminSystem } from '@todayz/api-client/gen/admin-system/admin-system';
import { useQuery } from '@tanstack/react-query';

/** The API caches each panel for 60 s (06 A-22), so the page asks again about as often. */
const REFRESH_MS = 60_000;

export const systemKey = ['admin', 'system'] as const;

export function useSystem() {
  return useQuery({ queryKey: systemKey, queryFn: () => getAdminSystem(), refetchInterval: REFRESH_MS });
}
