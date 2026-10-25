import type { JobClass } from '@todayz/api-client/gen/model/jobClass';
import type { JobHealth } from '@todayz/api-client/gen/model/jobHealth';
import type { JobRunStatus } from '@todayz/api-client/gen/model/jobRunStatus';
import type { JobTrigger } from '@todayz/api-client/gen/model/jobTrigger';
import { ApiError } from '@todayz/api-client';
import type { PillTone } from '@todayz/ui';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';

const CLASS: Readonly<Record<JobClass, string>> = {
  timer: 'jobs.class.timer',
  money: 'jobs.class.money',
  notify: 'jobs.class.notify',
  marketing: 'jobs.class.marketing',
  ai: 'jobs.class.ai',
  rollup: 'jobs.class.rollup',
  maintenance: 'jobs.class.maintenance',
};

const HEALTH: Readonly<Record<JobHealth, string>> = {
  ok: 'jobs.health.ok',
  failing: 'jobs.health.failing',
  missed: 'jobs.health.missed',
  paused: 'jobs.health.paused',
};

const HEALTH_TONE: Readonly<Record<JobHealth, PillTone>> = { ok: 'completed', failing: 'rejected', missed: 'pending', paused: 'neutral' };

const STATUS: Readonly<Record<JobRunStatus, string>> = {
  running: 'jobs.status.running',
  succeeded: 'jobs.status.succeeded',
  failed: 'jobs.status.failed',
  cancelled: 'jobs.status.cancelled',
  skipped: 'jobs.status.skipped',
  discarded: 'jobs.status.discarded',
  missed: 'jobs.status.missed',
  yielded: 'jobs.status.yielded',
};

const STATUS_TONE: Readonly<Record<JobRunStatus, PillTone>> = {
  running: 'preparing',
  succeeded: 'completed',
  failed: 'rejected',
  cancelled: 'cancelled',
  skipped: 'neutral',
  discarded: 'rejected',
  missed: 'pending',
  yielded: 'neutral',
};

const TRIGGER: Readonly<Record<JobTrigger, string>> = {
  schedule: 'jobs.trigger.schedule',
  event: 'jobs.trigger.event',
  manual: 'jobs.trigger.manual',
  retry: 'jobs.trigger.retry',
  backfill: 'jobs.trigger.backfill',
  continuation: 'jobs.trigger.continuation',
};

export function useJobLabels() {
  const intl = useIntl();
  return useMemo(
    () => ({
      cls: (c: JobClass) => intl.formatMessage({ id: CLASS[c] }),
      health: (h: JobHealth) => intl.formatMessage({ id: HEALTH[h] }),
      healthTone: (h: JobHealth) => HEALTH_TONE[h],
      status: (s: JobRunStatus) => intl.formatMessage({ id: STATUS[s] }),
      statusTone: (s: JobRunStatus) => STATUS_TONE[s],
      trigger: (t: JobTrigger) => intl.formatMessage({ id: TRIGGER[t] }),
      /** A duration in ms as "850 ms", "12.4 s" or "3 min 5 s". */
      duration: (ms: number | null | undefined) => {
        if (ms === null || ms === undefined) return intl.formatMessage({ id: 'jobs.none' });
        if (ms < 1000) return intl.formatMessage({ id: 'jobs.ms' }, { n: Math.round(ms) });
        if (ms < 60_000) return intl.formatMessage({ id: 'jobs.s' }, { n: Math.round(ms / 100) / 10 });
        return intl.formatMessage({ id: 'jobs.min' }, { m: Math.floor(ms / 60_000), s: Math.round((ms % 60_000) / 1000) });
      },
      /** Basis points as a percent with one decimal ("98.5%"). */
      rate: (bp: number | null | undefined) =>
        bp === null || bp === undefined ? intl.formatMessage({ id: 'jobs.none' }) : intl.formatNumber(bp / 10_000, { style: 'percent', maximumFractionDigits: 1 }),
    }),
    [intl],
  );
}

/** The job API's own refusals in words; anything else as every admin screen says it. */
export function useJobProblemText() {
  const intl = useIntl();
  const common = useProblemText();
  return useCallback(
    (err: unknown): string => {
      if (err instanceof ApiError) {
        switch (err.code) {
          case 'job_not_runnable':
            return intl.formatMessage({ id: 'jobs.error.notRunnable' });
          case 'job_paused':
            return intl.formatMessage({ id: 'jobs.error.paused' });
          case 'job_already_running':
            return intl.formatMessage({ id: 'jobs.error.running' });
          case 'step_up_required':
            return intl.formatMessage({ id: 'jobs.error.stepUp' });
        }
      }
      return common(err);
    },
    [intl, common],
  );
}
