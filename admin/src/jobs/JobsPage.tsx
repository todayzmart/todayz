import type { JobClass } from '@todayz/api-client/gen/model/jobClass';
import type { JobHealth } from '@todayz/api-client/gen/model/jobHealth';
import type { JobKindSummary } from '@todayz/api-client/gen/model/jobKindSummary';
import { Button, DataTable, Segmented, StatusPill, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { useJobLabels } from './labels';
import { useJobs } from './queries';
import { scheduleLabel } from './schedule';
import { CLASSES, HEALTHS, type JobsSearch } from './search';
import css from '../app/page.module.css';
import own from './jobs.module.css';

type Health = JobHealth | 'all';

/**
 * A-21 Jobs: every registered kind with its schedule in IST, next and last run, 7-day success rate
 * and durations, and a badge (Missed, Failing, Paused). Class and badge filters live in the URL.
 * finance sees `money` kinds only (the API filters); actions are on the kind page.
 */
export function JobsPage({ search }: { search: JobsSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const navigate = useNavigate();
  const labels = useJobLabels();
  const problemText = useProblemText();
  const go = (next: JobsSearch) => void navigate({ to: '/jobs', search: next });
  const q = useJobs(search.class, search.status);
  const rows = q.data?.items ?? [];
  const now = q.data?.serverNow;
  const none = intl.formatMessage({ id: 'jobs.none' });

  const columns = useMemo<ColumnDef<JobKindSummary, unknown>[]>(
    () => [
      {
        id: 'kind',
        header: intl.formatMessage({ id: 'jobs.col.kind' }),
        cell: ({ row }) => (
          <div className={own.kind}>
            <Link to="/jobs/$kind" params={{ kind: row.original.kind }} className={own.mono}>
              {row.original.kind}
            </Link>
            <span className={css.muted}>{row.original.description}</span>
          </div>
        ),
      },
      // Status sits next to the job so Missed, Failing and Paused are seen without scrolling.
      {
        id: 'health',
        header: intl.formatMessage({ id: 'jobs.col.health' }),
        cell: ({ row }) => <StatusPill tone={labels.healthTone(row.original.health)}>{labels.health(row.original.health)}</StatusPill>,
      },
      { id: 'class', header: intl.formatMessage({ id: 'jobs.col.class' }), accessorFn: (j) => labels.cls(j.class) },
      {
        id: 'schedule',
        header: intl.formatMessage({ id: 'jobs.col.schedule' }),
        accessorFn: (j) => (j.scheduled ? scheduleLabel(j.schedule, j.scheduleText) : intl.formatMessage({ id: 'jobs.event' })),
      },
      {
        id: 'next',
        header: intl.formatMessage({ id: 'jobs.col.next' }),
        accessorFn: (j) => (j.nextRunAt && now ? when(j.nextRunAt, now) : none),
      },
      {
        id: 'last',
        header: intl.formatMessage({ id: 'jobs.col.last' }),
        cell: ({ row }) => {
          const r = row.original.lastRun;
          if (!r || !now) return none;
          return (
            <span className={own.inline}>
              <StatusPill tone={labels.statusTone(r.status)}>{labels.status(r.status)}</StatusPill>
              <span className={css.muted}>{when(r.finishedAt ?? r.createdAt, now)}</span>
            </span>
          );
        },
      },
      {
        id: 'rate',
        header: intl.formatMessage({ id: 'jobs.col.rate' }),
        accessorFn: (j) => labels.rate(j.successRate7dBp),
        meta: { numeric: true },
      },
      {
        id: 'duration',
        header: intl.formatMessage({ id: 'jobs.col.duration' }),
        accessorFn: (j) => `${labels.duration(j.avgDurationMs7d)} / ${labels.duration(j.maxDurationMs7d)}`,
        meta: { numeric: true },
      },
    ],
    [intl, labels, when, now, none],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.jobs' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'jobs.sub' })}</p>
      </div>
      <div className={own.filters}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="jobs-class">
            {intl.formatMessage({ id: 'jobs.filter.class' })}
          </label>
          <select
            id="jobs-class"
            className="tz-input"
            value={search.class ?? ''}
            onChange={(e) => {
              const c = e.target.value as JobClass | '';
              go({ ...(c ? { class: c } : {}), ...(search.status ? { status: search.status } : {}) });
            }}
          >
            <option value="">{intl.formatMessage({ id: 'jobs.filter.anyClass' })}</option>
            {CLASSES.map((c) => (
              <option key={c} value={c}>
                {labels.cls(c)}
              </option>
            ))}
          </select>
        </div>
        <Segmented<Health>
          label={intl.formatMessage({ id: 'jobs.filter.health' })}
          options={[{ value: 'all', label: intl.formatMessage({ id: 'jobs.filter.anyHealth' }) }, ...HEALTHS.map((h) => ({ value: h, label: labels.health(h) }))]}
          value={search.status ?? 'all'}
          onChange={(h) => go({ ...(search.class ? { class: search.class } : {}), ...(h !== 'all' ? { status: h } : {}) })}
        />
      </div>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'jobs.table' })}
          columns={columns}
          data={rows}
          getRowId={(j) => j.kind}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: search.class || search.status ? 'jobs.emptyFiltered' : 'jobs.empty' })}
        />
      )}
    </div>
  );
}
