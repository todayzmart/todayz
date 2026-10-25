import { approveAdminBanner, listAdminBanners, takedownAdminBanner } from '@todayz/api-client/gen/admin-banners/admin-banners';
import type { ListAdminBannersParams } from '@todayz/api-client/gen/model/listAdminBannersParams';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

type BannerFilter = Omit<ListAdminBannersParams, 'limit' | 'cursor'>;

export const bannersKey = (f: BannerFilter) => ['admin', 'banners', f] as const;

export function useBanners(f: BannerFilter) {
  return useInfiniteQuery({
    queryKey: bannersKey(f),
    queryFn: ({ pageParam }) => listAdminBanners({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export type Decision = { kind: 'approve'; reason?: string } | { kind: 'takedown'; reason: string };

/**
 * Clear a held upload or take a banner down. Not idempotency-keyed: a repeat is
 * `409 invalid_transition`, so a retry can never apply it twice.
 */
export function useDecide(bannerId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (d: Decision) =>
      d.kind === 'approve'
        ? approveAdminBanner(bannerId, d.reason ? { reason: d.reason } : {})
        : takedownAdminBanner(bannerId, { reason: d.reason }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'banners'] });
      refreshAudit(qc, 'banner', bannerId);
    },
  });
}
