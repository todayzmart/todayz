import type { CreditLine } from './api';

/** The sign a line has in the shop's credit account, from its kind (01 §7 to §9). */
export function lineSign(line: Pick<CreditLine, 'kind' | 'credits'>): 1 | -1 {
  switch (line.kind) {
    case 'spent':
    case 'referral_reward':
      return 1;
    case 'cashback':
    case 'pos_reversal':
    case 'payout':
      return -1;
    default:
      return line.credits < 0 ? -1 : 1;
  }
}

/** The signed amount, e.g. +30 or −12. Uses a true minus sign. */
export function signedCredits(line: Pick<CreditLine, 'kind' | 'credits'>): string {
  const n = Math.abs(Math.trunc(line.credits));
  return `${lineSign(line) === 1 ? '+' : '−'}${n.toLocaleString('en-IN')}`;
}

const KINDS = new Set(['spent', 'cashback', 'referral_reward', 'pos_reversal', 'payout', 'adjustment']);

/** The i18n id for a line's label; unknown kinds fall back to "Adjustment". */
export function lineLabelId(kind: string): string {
  return `credits.line.${KINDS.has(kind) ? kind : 'adjustment'}`;
}
