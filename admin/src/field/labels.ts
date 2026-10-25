import { ApiError } from '@todayz/api-client';
import type { FieldBusinessStatus } from '@todayz/api-client/gen/model/fieldBusinessStatus';
import type { FieldUnit } from '@todayz/api-client/gen/model/fieldUnit';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

const STATUS: Record<FieldBusinessStatus, string> = {
  draft: 'field.status.draft',
  submitted: 'field.status.submitted',
  needs_info: 'field.status.needsInfo',
  verified: 'field.status.verified',
  rejected: 'field.status.rejected',
  suspended: 'field.status.suspended',
};

export const UNITS = ['g', 'kg', 'ml', 'l', 'pc', 'pack'] as const satisfies readonly FieldUnit[];

export const businessTone = (s: FieldBusinessStatus): PillTone =>
  s === 'verified' ? 'completed' : s === 'submitted' || s === 'needs_info' ? 'pending' : s === 'draft' ? 'neutral' : 'rejected';

/** Draft and needs-info shops are still the agent's to change before review. */
export const isOpen = (s: FieldBusinessStatus) => s === 'draft' || s === 'needs_info';

export function useFieldLabels() {
  const intl = useIntl();
  return useMemo(
    () => ({
      status: (s: FieldBusinessStatus) => intl.formatMessage({ id: STATUS[s] }),
      percent: (r: number | null) =>
        r === null ? '—' : intl.formatNumber(r, { style: 'percent', maximumFractionDigits: 0 }),
    }),
    [intl],
  );
}

/**
 * "9876543210", "09876543210" or "+91 98765 43210" -> "+919876543210"; anything else is null.
 * The number lives only in the form and the request body, never in a URL or a log.
 */
export function toE164(raw: string): string | null {
  let d = raw.replace(/[\s-]/g, '');
  if (d.startsWith('+91')) d = d.slice(3);
  else if (d.length === 11 && d.startsWith('0')) d = d.slice(1);
  return /^[6-9][0-9]{9}$/.test(d) ? `+91${d}` : null;
}

/** "45.50" -> 4550; empty or not a positive amount -> null. */
export function toPaise(raw: string): number | null {
  const t = raw.trim();
  if (!/^\d{1,7}(\.\d{1,2})?$/.test(t)) return null;
  const p = Math.round(Number(t) * 100);
  return p > 0 ? p : null;
}

export type CodeError = { kind: 'wrong'; attemptsLeft?: number } | { kind: 'locked' | 'expired' | 'wait' | 'gone' | 'other' };

/** The owner-code answers (04 §2.7): code_invalid 422, otp_locked 429, otp_expired 401, rate_limited 429. */
export function codeError(e: unknown): CodeError & { retryAfter?: number } {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 'code_invalid': {
        const left = e.problem.extensions?.['attemptsLeft'];
        return typeof left === 'number' ? { kind: 'wrong', attemptsLeft: left } : { kind: 'wrong' };
      }
      case 'otp_locked':
        return { kind: 'locked' };
      case 'otp_expired':
        return { kind: 'expired' };
      case 'rate_limited':
        return e.retryAfterSeconds ? { kind: 'wait', retryAfter: e.retryAfterSeconds } : { kind: 'wait' };
      case 'invalid_transition':
        return { kind: 'gone' };
    }
  }
  return { kind: 'other' };
}
