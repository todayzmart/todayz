import { JobClass } from '@todayz/api-client/gen/model/jobClass';
import { JobHealth } from '@todayz/api-client/gen/model/jobHealth';
import { JobRunStatus } from '@todayz/api-client/gen/model/jobRunStatus';
import { JobTrigger } from '@todayz/api-client/gen/model/jobTrigger';
import { day, text, ulid } from '../app/search';

export const CLASSES = Object.values(JobClass);
export const HEALTHS = Object.values(JobHealth);
export const RUN_STATUSES = Object.values(JobRunStatus);
export const TRIGGERS = Object.values(JobTrigger);

/** The contract's JobKindName pattern. */
export const KIND = /^[a-z][a-z0-9]*(_[a-z0-9]+)*$/;

const pick = <T extends string>(all: readonly T[], v: unknown): T | undefined => {
  const s = text(v);
  return (all as readonly string[]).includes(s) ? (s as T) : undefined;
};

/** A-21 list, kept in the URL: the class and the health badge. Absent is every one. */
export interface JobsSearch {
  class?: JobClass;
  status?: JobHealth;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateJobsSearch(raw: Record<string, unknown>): JobsSearch {
  const out: JobsSearch = {};
  const c = pick(CLASSES, raw.class);
  if (c) out.class = c;
  const s = pick(HEALTHS, raw.status);
  if (s) out.status = s;
  return out;
}

/** A kind's run history, kept in the URL: status, trigger, IST days, and the run open beside it. */
export interface KindSearch {
  status?: JobRunStatus;
  trigger?: JobTrigger;
  /** IST days (YYYY-MM-DD), `to` inclusive here and sent as the next midnight. */
  from?: string;
  to?: string;
  run?: string;
}

export function validateKindSearch(raw: Record<string, unknown>): KindSearch {
  const out: KindSearch = {};
  const s = pick(RUN_STATUSES, raw.status);
  if (s) out.status = s;
  const t = pick(TRIGGERS, raw.trigger);
  if (t) out.trigger = t;
  const from = day(raw.from);
  if (from) out.from = from;
  const to = day(raw.to);
  if (to) out.to = to;
  const run = ulid(raw.run);
  if (run) out.run = run;
  return out;
}
