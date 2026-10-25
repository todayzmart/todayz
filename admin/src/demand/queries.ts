import { decideSiteMarquee, listDemandGaps, listSiteMarquee } from '@todayz/api-client/gen/admin-demand/admin-demand';
import type { MarqueeDecision } from '@todayz/api-client/gen/model/marqueeDecision';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { refreshAudit } from '../audit/queries';

export const gapsKey = ['admin', 'demand', 'gaps'] as const;
export const marqueeKey = ['admin', 'demand', 'marquee'] as const;

/**
 * Every city's gaps at once (at most 500 rows, no pages); the city picker filters them here so it
 * can list the cities that have any.
 */
export const useGaps = () => useQuery({ queryKey: gapsKey, queryFn: () => listDemandGaps() });

export const useMarquee = (enabled: boolean) => useQuery({ queryKey: marqueeKey, queryFn: () => listSiteMarquee(), enabled });

/** Approve a subset of a city's proposed strip (audited as `marquee.decide`). */
export function useDecide(cityId: string) {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (body: MarqueeDecision) => once((headers) => decideSiteMarquee(cityId, body, { headers })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: marqueeKey });
      refreshAudit(qc, 'site_marquee', cityId);
    },
  });
}
