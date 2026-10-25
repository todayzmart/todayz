import type { RiskEvidenceKind } from '@todayz/api-client/gen/model/riskEvidenceKind';
import type { RiskFlagStatus } from '@todayz/api-client/gen/model/riskFlagStatus';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

/** A rule key, joined so the message lint does not read it as a message id. */
const rk = (domain: string, name: string) => [domain, name].join('.');

/** The rules the console names (D-225 and the packages that raise flags); any other shows as written. */
const RULES: Readonly<Record<string, string>> = {
  [rk('orders', 'code_lockouts')]: 'risk.rule.codeLockouts',
  [rk('pos', 'bill_claim_spread')]: 'risk.rule.billClaimSpread',
  [rk('pos', 'reversal_ratio')]: 'risk.rule.reversalRatio',
  [rk('pos', 'offline_links')]: 'risk.rule.offlineLinks',
  [rk('pos', 'offline_offer_without_discount_role')]: 'risk.rule.offlineOffer',
  [rk('referral', 'self_like')]: 'risk.rule.selfLike',
  [rk('referral', 'device_accounts')]: 'risk.rule.deviceAccounts',
  [rk('referral', 'reversed_after_reward')]: 'risk.rule.reversedAfterReward',
  [rk('referral', 'reward_refused')]: 'risk.rule.rewardRefused',
  [rk('identity', 'otp_ip_fanout')]: 'risk.rule.otpIpFanout',
  [rk('identity', 'otp_failed_phone')]: 'risk.rule.otpFailedPhone',
  [rk('credits', 'store_daily_cap')]: 'risk.rule.storeDailyCap',
  [rk('credits', 'customer_monthly_cap')]: 'risk.rule.customerMonthlyCap',
  store_strikes: 'risk.rule.storeStrikes',
  return_unconfirmed: 'risk.rule.returnUnconfirmed',
  ask_reported: 'risk.rule.askReported', // raised by W3-16's "Report this ask" (01 §11)
};

const SUBJECTS: Readonly<Record<string, string>> = {
  store: 'risk.subject.store',
  user: 'risk.subject.user',
  referral: 'risk.subject.referral',
  business: 'risk.subject.business',
  ip: 'risk.subject.ip',
  phone: 'risk.subject.phone',
};

const EVIDENCE: Readonly<Record<RiskEvidenceKind, string>> = {
  order: 'risk.evidence.order',
  pos_sale: 'risk.evidence.posSale',
  bill_claim: 'risk.evidence.billClaim',
  strike: 'risk.evidence.strike',
  code_lockout: 'risk.evidence.codeLockout',
  referral: 'risk.evidence.referral',
  otp_request: 'risk.evidence.otpRequest',
};

/** The rule whose clearing lets a held reward post on the next sweep (D-225). */
export const REWARD_REFUSED = rk('referral', 'reward_refused');

export function useRiskLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const named = (map: Readonly<Record<string, string>>, key: string) => {
      const id = map[key];
      return id ? intl.formatMessage({ id }) : key;
    };
    return {
      rule: (r: string): string => named(RULES, r),
      subjectType: (t: string): string => named(SUBJECTS, t),
      evidence: (k: RiskEvidenceKind): string => intl.formatMessage({ id: EVIDENCE[k] }),
      /** A flag's subject: its name, or for an ip or phone (no name, D-225) the kind alone. */
      subject: (t: string, label: string | null | undefined): string => label ?? named(SUBJECTS, t),
      tone: (s: RiskFlagStatus): PillTone => (s === 'open' ? 'pending' : s === 'confirmed' ? 'rejected' : 'completed'),
      status: (s: RiskFlagStatus): string => {
        switch (s) {
          case 'open':
            return intl.formatMessage({ id: 'risk.status.open' });
          case 'cleared':
            return intl.formatMessage({ id: 'risk.status.cleared' });
          case 'confirmed':
            return intl.formatMessage({ id: 'risk.status.confirmed' });
        }
      },
    };
  }, [intl]);
}
