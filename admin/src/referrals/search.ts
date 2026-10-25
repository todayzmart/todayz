import type { AdminReferralKind } from '@todayz/api-client/gen/model/adminReferralKind';
import type { AdminReferralStatus } from '@todayz/api-client/gen/model/adminReferralStatus';
import { text, ulid } from '../app/search';

export const STATUSES = ['pending', 'qualified', 'rewarded', 'rejected', 'rejected_same_device'] as const satisfies readonly AdminReferralStatus[];
export const KINDS = ['retailer_customer', 'customer_customer', 'any_shop'] as const satisfies readonly AdminReferralKind[];

/** A-10 view, kept in the URL: the filters and the referral open beside the list. */
export interface ReferralsSearch {
  /** Absent is every status. */
  status?: AdminReferralStatus;
  kind?: AdminReferralKind;
  /** Only referrals with an open risk flag (A-13). */
  flagged?: true;
  /** The referral shown in the side panel. */
  referral?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateReferralsSearch(raw: Record<string, unknown>): ReferralsSearch {
  const out: ReferralsSearch = {};
  const s = text(raw.status);
  if ((STATUSES as readonly string[]).includes(s)) out.status = s as AdminReferralStatus;
  const k = text(raw.kind);
  if ((KINDS as readonly string[]).includes(k)) out.kind = k as AdminReferralKind;
  if (raw.flagged === true || raw.flagged === 'true') out.flagged = true;
  const id = ulid(raw.referral);
  if (id) out.referral = id;
  return out;
}
