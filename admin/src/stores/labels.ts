import type { AdminStoreStatus } from '@todayz/api-client/gen/model/adminStoreStatus';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

/** Store statuses (03 §3), in the order the filter lists them. */
export const STORE_STATUSES = ['draft', 'pending_review', 'live', 'paused', 'suspended'] as const satisfies readonly AdminStoreStatus[];

const STATUS: Record<AdminStoreStatus, string> = {
  draft: 'stores.status.draft',
  pending_review: 'stores.status.pendingReview',
  live: 'stores.status.live',
  paused: 'stores.status.paused',
  suspended: 'stores.status.suspended',
};

/** Weekdays as the hours JSON names them, Monday first. */
export const WEEKDAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
export type Weekday = (typeof WEEKDAYS)[number];

const DAY: Record<Weekday, string> = {
  mon: 'stores.day.mon',
  tue: 'stores.day.tue',
  wed: 'stores.day.wed',
  thu: 'stores.day.thu',
  fri: 'stores.day.fri',
  sat: 'stores.day.sat',
  sun: 'stores.day.sun',
};

const STRIKE: Record<string, string> = {
  no_show: 'stores.strike.noShow',
  dispatch_timeout: 'stores.strike.dispatchTimeout',
  admin_cancel: 'stores.strike.adminCancel',
};

export const storeTone = (s: AdminStoreStatus): PillTone =>
  s === 'live' ? 'completed' : s === 'pending_review' ? 'pending' : s === 'suspended' ? 'rejected' : 'neutral';

export function useStoreLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const t = (id: string) => intl.formatMessage({ id });
    return {
      status: (s: AdminStoreStatus) => t(STATUS[s]),
      day: (d: Weekday) => t(DAY[d]),
      /** A strike reason the console does not know yet shows as written. */
      strike: (r: string) => (STRIKE[r] ? t(STRIKE[r]) : r),
      /** 0..1 as a whole percentage, or a dash. */
      percent: (v: number | null) => (v === null ? '—' : intl.formatNumber(v, { style: 'percent', maximumFractionDigits: 0 })),
    };
  }, [intl]);
}
