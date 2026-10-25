import {
  getAdminJob,
  getAdminJobRun,
  listAdminJobRuns,
  listAdminJobs,
  pauseAdminJob,
  resumeAdminJob,
  retryAdminJobRun,
  runAdminJob,
} from '@todayz/api-client/gen/admin-jobs/admin-jobs';
import type { JobClass } from '@todayz/api-client/gen/model/jobClass';
import type { JobControlState } from '@todayz/api-client/gen/model/jobControlState';
import type { JobHealth } from '@todayz/api-client/gen/model/jobHealth';
import type { JobRunAccepted } from '@todayz/api-client/gen/model/jobRunAccepted';
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { dayEnd, dayStart } from '../app/search';
import { PAGE, refreshAudit } from '../audit/queries';
import type { KindSearch } from './search';

/** The list and a kind's health move with the worker, so they refresh while the page is open. */
const LIVE_MS = 30_000;

export const jobsKey = (cls?: JobClass, status?: JobHealth) => ['admin', 'jobs', 'list', cls ?? '', status ?? ''] as const;
export const jobKey = (kind: string) => ['admin', 'jobs', 'kind', kind] as const;
export const runsKey = (kind: string, s: KindSearch) => ['admin', 'jobs', 'runs', kind, s.status ?? '', s.trigger ?? '', s.from ?? '', s.to ?? ''] as const;
export const runKey = (id: string) => ['admin', 'jobs', 'run', id] as const;

export function useJobs(cls?: JobClass, status?: JobHealth) {
  return useQuery({
    queryKey: jobsKey(cls, status),
    queryFn: () => listAdminJobs({ ...(cls ? { class: cls } : {}), ...(status ? { status } : {}) }),
    refetchInterval: LIVE_MS,
    placeholderData: keepPreviousData,
  });
}

export function useJob(kind: string) {
  return useQuery({ queryKey: jobKey(kind), queryFn: () => getAdminJob(kind), refetchInterval: LIVE_MS });
}

export function useRuns(kind: string, s: KindSearch) {
  return useInfiniteQuery({
    queryKey: runsKey(kind, s),
    queryFn: ({ pageParam }) =>
      listAdminJobRuns(kind, {
        ...(s.status ? { status: s.status } : {}),
        ...(s.trigger ? { trigger: s.trigger } : {}),
        ...(s.from ? { from: dayStart(s.from) } : {}),
        ...(s.to ? { to: dayEnd(s.to) } : {}),
        limit: PAGE,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useRun(id: string) {
  return useQuery({ queryKey: runKey(id), queryFn: () => getAdminJobRun(id) });
}

export type JobMove = { kind: 'run'; reason: string; periodKey?: string } | { kind: 'pause' | 'resume'; reason: string };

/**
 * Run now, pause and resume (each audited on the kind, `job_kind`). One Idempotency-Key per
 * change, so a network retry cannot queue a second run. A `money` kind answers
 * `403 step_up_required` until the super_admin has a fresh second factor.
 */
export function useJobMove(job: string) {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (m: JobMove) =>
      once<JobRunAccepted | JobControlState>((headers) => {
        if (m.kind === 'run') return runAdminJob(job, { reason: m.reason, ...(m.periodKey ? { periodKey: m.periodKey } : {}) }, { headers });
        return (m.kind === 'pause' ? pauseAdminJob : resumeAdminJob)(job, { reason: m.reason }, { headers });
      }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'jobs'] });
      refreshAudit(qc, 'job_kind', job);
    },
  });
}

/** Retry a failed, discarded or missed run (audited on the run, `job_run`). */
export function useRetry(job: string, runId: string) {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (reason: string) => once((headers) => retryAdminJobRun(runId, { reason }, { headers })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'jobs'] });
      refreshAudit(qc, 'job_run', runId);
      refreshAudit(qc, 'job_kind', job);
    },
  });
}
