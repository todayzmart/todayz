import type { PillTone } from '@todayz/ui';
import { ApiError } from '@todayz/api-client';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';

export const statusTone = (s: string): PillTone =>
  s === 'paid' ? 'paid' : s === 'held' ? 'rejected' : s === 'cancelled' ? 'cancelled' : 'pending';

export function usePayoutLabels() {
  const intl = useIntl();
  const status = useCallback(
    (s: string): string => {
      switch (s) {
        case 'pending':
          return intl.formatMessage({ id: 'payouts.status.pending' });
        case 'held':
          return intl.formatMessage({ id: 'payouts.status.held' });
        case 'paid':
          return intl.formatMessage({ id: 'payouts.status.paid' });
        case 'cancelled':
          return intl.formatMessage({ id: 'payouts.status.cancelled' });
        default:
          return s;
      }
    },
    [intl],
  );
  const hold = useCallback(
    (h: string | null): string => {
      switch (h) {
        case 'shop_unchecked':
          return intl.formatMessage({ id: 'payouts.hold.unchecked' });
        case 'kyc_incomplete':
          return intl.formatMessage({ id: 'payouts.hold.kyc' });
        case 'payout_details_missing':
          return intl.formatMessage({ id: 'payouts.hold.details' });
        case 'risk_flag':
          return intl.formatMessage({ id: 'payouts.hold.risk' });
        case 'business_suspended':
          return intl.formatMessage({ id: 'payouts.hold.suspended' });
        case 'manual':
          return intl.formatMessage({ id: 'payouts.hold.manual' });
        default:
          return intl.formatMessage({ id: 'payouts.none' });
      }
    },
    [intl],
  );
  const kyc = useCallback(
    (k: string): string => {
      switch (k) {
        case 'verified':
          return intl.formatMessage({ id: 'payouts.kyc.verified' });
        case 'submitted':
          return intl.formatMessage({ id: 'payouts.kyc.submitted' });
        case 'rejected':
          return intl.formatMessage({ id: 'payouts.kyc.rejected' });
        default:
          return intl.formatMessage({ id: 'payouts.kyc.missing' });
      }
    },
    [intl],
  );
  return useMemo(() => ({ status, hold, kyc }), [status, hold, kyc]);
}

/** The console's problem sentences plus the payout codes (problems/admin-payouts.yaml). */
export function usePayoutProblemText() {
  const intl = useIntl();
  const common = useProblemText();
  return useCallback(
    (err: unknown): string => {
      if (err instanceof ApiError) {
        switch (err.code) {
          case 'payout_not_payable':
            return intl.formatMessage({ id: 'payouts.problem.notPayable' });
          case 'payout_already_paid':
            return intl.formatMessage({ id: 'payouts.problem.alreadyPaid' });
          case 'utr_in_use':
            return intl.formatMessage({ id: 'payouts.problem.utrInUse' });
          case 'payout_hold_cause_open':
            return intl.formatMessage({ id: 'payouts.problem.causeOpen' });
          case 'payout_amount_changed':
            return intl.formatMessage({ id: 'payouts.problem.amountChanged' });
        }
      }
      return common(err);
    },
    [intl, common],
  );
}
