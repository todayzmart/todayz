import { describe, expect, it } from 'vitest';
import { lineLabelId, lineSign, signedCredits } from './lines';

describe('credit line signs', () => {
  it('adds for spent and referral rewards, takes off for cashback, reversals and payouts', () => {
    expect(signedCredits({ kind: 'spent', credits: 30 })).toBe('+30');
    expect(signedCredits({ kind: 'referral_reward', credits: 100 })).toBe('+100');
    expect(signedCredits({ kind: 'cashback', credits: 12 })).toBe('−12');
    expect(signedCredits({ kind: 'pos_reversal', credits: 30 })).toBe('−30');
    expect(signedCredits({ kind: 'payout', credits: 1500 })).toBe('−1,500');
  });
  it('follows the value for an adjustment and for unknown kinds', () => {
    expect(lineSign({ kind: 'adjustment', credits: -5 })).toBe(-1);
    expect(lineSign({ kind: 'adjustment', credits: 5 })).toBe(1);
    expect(lineLabelId('mystery')).toBe('credits.line.adjustment');
    expect(lineLabelId('cashback')).toBe('credits.line.cashback');
  });
});
