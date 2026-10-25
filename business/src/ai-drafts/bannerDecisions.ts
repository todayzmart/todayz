import type { BannerCopyDecision } from '@todayz/api-client/gen/model/bannerCopyDecision';
import type { BannerCopyDraft } from '@todayz/api-client/gen/model/bannerCopyDraft';

/** The picked option is `accepted`, every other one `discarded`; no pick discards them all. */
export function bannerDecisions(d: BannerCopyDraft, pick: string | null): BannerCopyDecision[] {
  return d.lines.map((l) => ({ lineId: l.lineId, outcome: l.lineId === pick ? 'accepted' : 'discarded' }));
}
