import type { PillTone } from '@todayz/ui';
import { ApiError } from '@todayz/api-client';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';

export const statusTone = (s: string): PillTone =>
  s === 'approved' ? 'completed' : s === 'rejected' ? 'rejected' : s === 'cancelled' ? 'cancelled' : 'pending';

const ACCOUNT = {
  customer_wallet: 'credits.account.customer_wallet',
  customer_held: 'credits.account.customer_held',
  store_credit: 'credits.account.store_credit',
  platform_marketing: 'credits.account.platform_marketing',
  platform_expired: 'credits.account.platform_expired',
  platform_adjustments: 'credits.account.platform_adjustments',
  platform_payouts_cash: 'credits.account.platform_payouts_cash',
} as const;

const TX = {
  spend_hold: 'credits.tx.spend_hold',
  spend_capture: 'credits.tx.spend_capture',
  spend_release: 'credits.tx.spend_release',
  cashback: 'credits.tx.cashback',
  referral_reward: 'credits.tx.referral_reward',
  expiry: 'credits.tx.expiry',
  admin_adjustment: 'credits.tx.admin_adjustment',
  payout: 'credits.tx.payout',
  reversal: 'credits.tx.reversal',
  forfeit_deletion: 'credits.tx.forfeit_deletion',
} as const;

const STATUS = {
  pending: 'credits.status.pending',
  approved: 'credits.status.approved',
  rejected: 'credits.status.rejected',
  cancelled: 'credits.status.cancelled',
} as const;

const has = <M extends object>(m: M, k: string): k is Extract<keyof M, string> => Object.hasOwn(m, k);

export function useCreditLabels() {
  const intl = useIntl();
  const account = useCallback((k: string) => (has(ACCOUNT, k) ? intl.formatMessage({ id: ACCOUNT[k] }) : k), [intl]);
  const tx = useCallback((k: string) => (has(TX, k) ? intl.formatMessage({ id: TX[k] }) : k), [intl]);
  const status = useCallback((k: string) => (has(STATUS, k) ? intl.formatMessage({ id: STATUS[k] }) : k), [intl]);
  return useMemo(() => ({ account, tx, status }), [account, tx, status]);
}

/** The console's problem sentences plus the credit codes (problems/admin-credits.yaml). */
export function useCreditProblemText() {
  const intl = useIntl();
  const common = useProblemText();
  return useCallback(
    (err: unknown): string => {
      if (err instanceof ApiError) {
        switch (err.code) {
          case 'approver_is_requester':
            return intl.formatMessage({ id: 'credits.problem.approverIsRequester' });
          case 'adjustment_insufficient_balance':
            return intl.formatMessage({ id: 'credits.problem.insufficient' });
        }
      }
      return common(err);
    },
    [intl, common],
  );
}
