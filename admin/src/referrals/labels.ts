import type { AdminReferralKind } from '@todayz/api-client/gen/model/adminReferralKind';
import type { AdminReferralPartyType } from '@todayz/api-client/gen/model/adminReferralPartyType';
import type { AdminReferralSource } from '@todayz/api-client/gen/model/adminReferralSource';
import type { AdminReferralStatus } from '@todayz/api-client/gen/model/adminReferralStatus';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

const STATUS: Readonly<Record<AdminReferralStatus, string>> = {
  pending: 'referrals.status.pending',
  qualified: 'referrals.status.qualified',
  rewarded: 'referrals.status.rewarded',
  rejected: 'referrals.status.rejected',
  rejected_same_device: 'referrals.status.rejectedSameDevice',
};

const TONE: Readonly<Record<AdminReferralStatus, PillTone>> = {
  pending: 'pending',
  qualified: 'accepted',
  rewarded: 'completed',
  rejected: 'rejected',
  rejected_same_device: 'rejected',
};

const KIND: Readonly<Record<AdminReferralKind, string>> = {
  retailer_customer: 'referrals.kind.retailerCustomer',
  customer_customer: 'referrals.kind.customerCustomer',
  any_shop: 'referrals.kind.anyShop',
};

const SOURCE: Readonly<Record<AdminReferralSource, string>> = {
  qr_standee: 'referrals.source.qrStandee',
  link: 'referrals.source.link',
  code: 'referrals.source.code',
};

const PARTY: Readonly<Record<AdminReferralPartyType, string>> = {
  store: 'referrals.party.store',
  customer: 'referrals.party.customer',
  business: 'referrals.party.business',
};

export function useReferralLabels() {
  const intl = useIntl();
  return useMemo(
    () => ({
      status: (s: AdminReferralStatus) => intl.formatMessage({ id: STATUS[s] }),
      tone: (s: AdminReferralStatus) => TONE[s],
      kind: (k: AdminReferralKind) => intl.formatMessage({ id: KIND[k] }),
      source: (s: AdminReferralSource) => intl.formatMessage({ id: SOURCE[s] }),
      /** A party's name, or its kind when it has none (never a phone number). */
      party: (t: AdminReferralPartyType, label: string | null) => label ?? intl.formatMessage({ id: PARTY[t] }),
    }),
    [intl],
  );
}
