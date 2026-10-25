import type { JobKindDetail } from '@todayz/api-client/gen/model/jobKindDetail';
import type { JobKindSettingsUniqueBy } from '@todayz/api-client/gen/model/jobKindSettingsUniqueBy';
import type { JobRun } from '@todayz/api-client/gen/model/jobRun';
import type { JobRunDetail } from '@todayz/api-client/gen/model/jobRunDetail';
import type { JobRunStatus } from '@todayz/api-client/gen/model/jobRunStatus';
import type { JobTrigger } from '@todayz/api-client/gen/model/jobTrigger';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, Skeleton, StatusPill, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { GRAFANA_URL } from '../app/env';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { StepUpDialog } from '../app/StepUpDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { logsLink, traceLink } from './explore';
import { jobGate } from './gate';
import { useJobLabels, useJobProblemText } from './labels';
import { useJob, useJobMove, useRetry, useRun, useRuns, type JobMove } from './queries';
import { scheduleLabel } from './schedule';
import { RUN_STATUSES, TRIGGERS, type KindSearch } from './search';
import css from '../app/page.module.css';
import own from './jobs.module.css';

/** Run-now's `periodKey` format per the kind's IST uniqueness period (JobRunNowRequest). */
const PERIOD: Readonly<Record<Exclude<JobKindSettingsUniqueBy, 'none'>, { re: RegExp; example: string }>> = {
  tick: { re: /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/, example: '2026-10-22T09:05' },
  hour: { re: /^\d{4}-\d{2}-\d{2}T\d{2}$/, example: '2026-10-22T09' },
  day: { re: /^\d{4}-\d{2}-\d{2}$/, example: '2026-10-22' },
  month: { re: /^\d{4}-\d{2}$/, example: '2026-10' },
};

const RETRYABLE: readonly JobRunStatus[] = ['failed', 'discarded', 'missed'];

/** Each change's button, dialog and toast copy. */
const MOVE: Readonly<Record<JobMove['kind'], { label: string; title: string; body: string; done: string }>> = {
  run: { label: 'jobs.act.run', title: 'jobs.act.run.title', body: 'jobs.act.run.body', done: 'jobs.done.run' },
  pause: { label: 'jobs.act.pause', title: 'jobs.act.pause.title', body: 'jobs.act.pause.body', done: 'jobs.done.pause' },
  resume: { label: 'jobs.act.resume', title: 'jobs.act.resume.title', body: 'jobs.act.resume.body', done: 'jobs.done.resume' },
};

/** The step-up dialog a `money` action opens on `403 step_up_required`, then the same change again. */
function useStepUp() {
  const [retry, setRetry] = useState<(() => void) | null>(null);
  const ask = useCallback((err: unknown, again: () => void) => {
    if (err instanceof ApiError && err.code === 'step_up_required') setRetry(() => again);
  }, []);
  const dialog = (
    <StepUpDialog
      open={retry !== null}
      onVerified={() => {
        const again = retry;
        setRetry(null);
        again?.();
      }}
      onClose={() => setRetry(null)}
    />
  );
  return { ask, done: () => setRetry(null), dialog };
}

export function KindPage({ kind, search }: { kind: string; search: KindSearch }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useJob(kind);
  if (q.isPending) return <Skeleton shape="block" height="320px" />;
  if (q.isError)
    return (
      <div className={css.column}>
        <BackLink />
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      </div>
    );
  return <Kind d={q.data} search={search} />;
}

function BackLink() {
  const intl = useIntl();
  return (
    <Link to="/jobs" className={css.muted}>
      {intl.formatMessage({ id: 'jobs.back' })}
    </Link>
  );
}

function Kind({ d, search }: { d: JobKindDetail; search: KindSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const navigate = useNavigate();
  const labels = useJobLabels();
  const jobProblem = useJobProblemText();
  const { job, settings, serverNow } = d;
  const gate = jobGate(roles, job.class);
  const move = useJobMove(job.kind);
  const step = useStepUp();
  const [open, setOpen] = useState<JobMove['kind'] | null>(null);
  const [period, setPeriod] = useState('');
  const none = intl.formatMessage({ id: 'jobs.none' });
  const periodRule = settings.uniqueBy === 'none' ? null : PERIOD[settings.uniqueBy];
  const periodBad = !!periodRule && period.trim() !== '' && !periodRule.re.test(period.trim());
  const go = (next: KindSearch) => void navigate({ to: '/jobs/$kind', params: { kind: job.kind }, search: next });
  const filters = (over: KindSearch): KindSearch => {
    const { run: _run, ...rest } = search;
    void _run;
    return { ...rest, ...over };
  };

  const close = () => {
    setOpen(null);
    setPeriod('');
    move.reset();
  };
  const send = (m: JobMove) =>
    move.mutate(m, {
      onSuccess: (res) => {
        step.done();
        close();
        toast.show({ message: intl.formatMessage({ id: MOVE[m.kind].done }) });
        if (m.kind === 'run' && res && 'runId' in res) go(filters({ run: res.runId }));
      },
      onError: (e) => step.ask(e, () => send(m)),
    });

  const copy = MOVE[open ?? 'run'];

  return (
    <div className={css.column}>
      <BackLink />
      <div className={css.head}>
        <div className={css.stack}>
          <div className={css.title}>
            <h1 className="tz-page-title">{job.kind}</h1>
            <StatusPill tone="neutral">{labels.cls(job.class)}</StatusPill>
            <StatusPill tone={labels.healthTone(job.health)}>{labels.health(job.health)}</StatusPill>
          </div>
          <p className="tz-page-sub">{job.description}</p>
        </div>
        <div className={css.actions}>
          {gate.timer ? (
            <p className={css.muted}>{intl.formatMessage({ id: 'jobs.timerNote' })}</p>
          ) : gate.moneyLocked ? (
            <p className={css.muted}>{intl.formatMessage({ id: 'jobs.moneyNote' })}</p>
          ) : gate.may ? (
            <>
              {job.runnable && !job.control.paused && (
                <Button variant="primary" onClick={() => setOpen('run')}>
                  {intl.formatMessage({ id: 'jobs.act.run' })}
                </Button>
              )}
              {job.pausable &&
                (job.control.paused ? (
                  <Button variant="soft" onClick={() => setOpen('resume')}>
                    {intl.formatMessage({ id: 'jobs.act.resume' })}
                  </Button>
                ) : (
                  <Button variant="soft" onClick={() => setOpen('pause')}>
                    {intl.formatMessage({ id: 'jobs.act.pause' })}
                  </Button>
                ))}
            </>
          ) : null}
        </div>
      </div>

      <div className={css.detail}>
        <div className={css.column}>
          <section className="tz-card" aria-labelledby="job-facts">
            <h2 id="job-facts" className="tz-card-title">
              {intl.formatMessage({ id: 'jobs.facts' })}
            </h2>
            <dl className={css.facts}>
              <dt>{intl.formatMessage({ id: 'jobs.fact.owner' })}</dt>
              <dd>{job.owner}</dd>
              <dt>{intl.formatMessage({ id: 'jobs.col.schedule' })}</dt>
              <dd>{job.scheduled ? scheduleLabel(job.schedule, job.scheduleText) : intl.formatMessage({ id: 'jobs.event' })}</dd>
              <dt>{intl.formatMessage({ id: 'jobs.col.next' })}</dt>
              <dd>{job.nextRunAt ? when(job.nextRunAt, serverNow) : none}</dd>
              <dt>{intl.formatMessage({ id: 'jobs.col.rate' })}</dt>
              <dd>
                {labels.rate(job.successRate7dBp)} · {intl.formatMessage({ id: 'jobs.fact.runs' }, { n: job.runs7d ?? 0 })}
              </dd>
              <dt>{intl.formatMessage({ id: 'jobs.col.duration' })}</dt>
              <dd>
                {labels.duration(job.avgDurationMs7d)} / {labels.duration(job.maxDurationMs7d)}
              </dd>
              <dt>{intl.formatMessage({ id: 'jobs.fact.limits' })}</dt>
              <dd>
                {intl.formatMessage(
                  { id: 'jobs.fact.limitsValue' },
                  { timeout: settings.timeoutSeconds, attempts: settings.maxAttempts, queue: settings.queue, concurrency: settings.concurrency },
                )}
              </dd>
              {settings.runbook && (
                <>
                  <dt>{intl.formatMessage({ id: 'jobs.fact.runbook' })}</dt>
                  <dd className={own.mono}>{settings.runbook}</dd>
                </>
              )}
              {job.control.paused && (
                <>
                  <dt>{intl.formatMessage({ id: 'jobs.fact.paused' })}</dt>
                  <dd>
                    {job.control.reason ?? none}
                    {job.control.updatedAt && <span className={css.muted}> · {when(job.control.updatedAt, serverNow)}</span>}
                    {job.control.updatedBy && <span className={`${css.muted} ${own.mono}`}> · {job.control.updatedBy}</span>}
                  </dd>
                </>
              )}
            </dl>
          </section>
          <Runs kind={job.kind} search={search} go={go} filters={filters} />
        </div>
        <div className={css.column}>
          {search.run ? (
            <RunPanel key={search.run} id={search.run} kind={job.kind} scheduled={job.scheduled} may={gate.may} />
          ) : (
            <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'jobs.pick' })}</p>
          )}
          {can(roles, 'jobAct') && <AuditPanel type="job_kind" id={job.kind} />}
        </div>
      </div>

      <ReasonDialog
        open={open !== null}
        title={intl.formatMessage({ id: copy.title }, { kind: job.kind })}
        description={intl.formatMessage({ id: copy.body })}
        confirmLabel={intl.formatMessage({ id: copy.label })}
        danger={open === 'pause'}
        pending={move.isPending}
        error={move.error && !(move.error instanceof ApiError && move.error.code === 'step_up_required') ? jobProblem(move.error) : null}
        blocked={periodBad}
        onClose={close}
        onConfirm={(reason) => {
          if (!open) return;
          const p = period.trim();
          send(open === 'run' ? { kind: 'run', reason, ...(p ? { periodKey: p } : {}) } : { kind: open, reason });
        }}
      >
        {open === 'run' && periodRule && (
          <TextField
            label={intl.formatMessage({ id: 'jobs.act.period' })}
            help={intl.formatMessage({ id: 'jobs.act.periodHelp' }, { example: periodRule.example })}
            value={period}
            maxLength={16}
            onChange={(e) => setPeriod(e.target.value)}
            {...(periodBad ? { error: intl.formatMessage({ id: 'jobs.act.periodBad' }, { example: periodRule.example }) } : {})}
          />
        )}
      </ReasonDialog>
      {step.dialog}
    </div>
  );
}

/** A kind's run history, newest first, with status, trigger and IST-day filters. */
function Runs({ kind, search, go, filters }: { kind: string; search: KindSearch; go: (s: KindSearch) => void; filters: (over: KindSearch) => KindSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useJobLabels();
  const problemText = useProblemText();
  const q = useRuns(kind, search);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const none = intl.formatMessage({ id: 'jobs.none' });
  const [from, setFrom] = useState(search.from ?? '');
  const [to, setTo] = useState(search.to ?? '');

  const pickStatus = (s: string) => {
    const { status: _s, run: _r, ...rest } = search;
    void _s;
    void _r;
    go({ ...rest, ...(s ? { status: s as JobRunStatus } : {}) });
  };
  const pickTrigger = (t: string) => {
    const { trigger: _t, run: _r, ...rest } = search;
    void _t;
    void _r;
    go({ ...rest, ...(t ? { trigger: t as JobTrigger } : {}) });
  };
  const applyDates = (e: FormEvent) => {
    e.preventDefault();
    const { from: _f, to: _t, run: _r, ...rest } = search;
    void _f;
    void _t;
    void _r;
    go({ ...rest, ...(from ? { from } : {}), ...(to ? { to } : {}) });
  };
  const clear = () => {
    setFrom('');
    setTo('');
    go({});
  };

  const columns = useMemo<ColumnDef<JobRun, unknown>[]>(
    () => [
      {
        id: 'status',
        header: intl.formatMessage({ id: 'jobs.run.status' }),
        cell: ({ row }) => (
          <Button size="sm" variant="ghost" aria-pressed={row.original.id === search.run} onClick={() => go(filters({ run: row.original.id }))}>
            <StatusPill tone={labels.statusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>
          </Button>
        ),
      },
      { id: 'trigger', header: intl.formatMessage({ id: 'jobs.run.trigger' }), accessorFn: (r) => labels.trigger(r.trigger) },
      { id: 'period', header: intl.formatMessage({ id: 'jobs.run.period' }), accessorFn: (r) => r.periodKey ?? none },
      {
        id: 'started',
        header: intl.formatMessage({ id: 'jobs.run.started' }),
        accessorFn: (r) => (r.startedAt ? when(r.startedAt, now ?? r.startedAt) : none),
      },
      { id: 'duration', header: intl.formatMessage({ id: 'jobs.run.duration' }), accessorFn: (r) => labels.duration(r.durationMs), meta: { numeric: true } },
      { id: 'late', header: intl.formatMessage({ id: 'jobs.run.late' }), accessorFn: (r) => labels.duration(r.latenessMs), meta: { numeric: true } },
      { id: 'summary', header: intl.formatMessage({ id: 'jobs.run.summary' }), accessorFn: (r) => summaryText(r.summary) || none },
    ],
    // go and filters are rebuilt each render from search, which is listed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [intl, labels, when, now, none, search],
  );

  return (
    <section className="tz-card" aria-labelledby="job-runs">
      <div className={css.stack}>
        <h2 id="job-runs" className="tz-card-title">
          {intl.formatMessage({ id: 'jobs.runs' })}
        </h2>
        <form className={own.filters} onSubmit={applyDates}>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="runs-status">
              {intl.formatMessage({ id: 'jobs.run.status' })}
            </label>
            <select id="runs-status" className="tz-input" value={search.status ?? ''} onChange={(e) => pickStatus(e.target.value)}>
              <option value="">{intl.formatMessage({ id: 'jobs.filter.anyStatus' })}</option>
              {RUN_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {labels.status(s)}
                </option>
              ))}
            </select>
          </div>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="runs-trigger">
              {intl.formatMessage({ id: 'jobs.run.trigger' })}
            </label>
            <select id="runs-trigger" className="tz-input" value={search.trigger ?? ''} onChange={(e) => pickTrigger(e.target.value)}>
              <option value="">{intl.formatMessage({ id: 'jobs.filter.anyTrigger' })}</option>
              {TRIGGERS.map((t) => (
                <option key={t} value={t}>
                  {labels.trigger(t)}
                </option>
              ))}
            </select>
          </div>
          <TextField type="date" label={intl.formatMessage({ id: 'jobs.filter.from' })} value={from} onChange={(e) => setFrom(e.target.value)} />
          <TextField type="date" label={intl.formatMessage({ id: 'jobs.filter.to' })} value={to} onChange={(e) => setTo(e.target.value)} />
          <Button type="submit" variant="soft">
            {intl.formatMessage({ id: 'jobs.filter.apply' })}
          </Button>
          {(search.status || search.trigger || search.from || search.to) && (
            <Button variant="ghost" onClick={clear}>
              {intl.formatMessage({ id: 'jobs.filter.clear' })}
            </Button>
          )}
        </form>
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'jobs.runs' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'jobs.runsEmpty' })}
            footer={
              q.hasNextPage ? (
                <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                  {intl.formatMessage({ id: 'common.loadMore' })}
                </Button>
              ) : undefined
            }
          />
        )}
      </div>
    </section>
  );
}

/** "sent 120 · failed 2": the handler's counts in its own order. */
function summaryText(s: JobRun['summary']): string {
  return s
    ? Object.entries(s)
        .map(([k, n]) => `${k} ${n}`)
        .join(' · ')
    : '';
}

function RunPanel({ id, kind, scheduled, may }: { id: string; kind: string; scheduled: boolean; may: boolean }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useRun(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError)
    return (
      <p role="alert" className="tz-card tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  return <RunCard d={q.data} kind={kind} scheduled={scheduled} may={may} />;
}

/** The chosen run: counts, note, scrubbed error, attempts, who asked for it, and where to look in Grafana. */
function RunCard({ d, kind, scheduled, may }: { d: JobRunDetail; kind: string; scheduled: boolean; may: boolean }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const labels = useJobLabels();
  const jobProblem = useJobProblemText();
  const { run: r, serverNow } = d;
  const retry = useRetry(kind, r.id);
  const step = useStepUp();
  const [asking, setAsking] = useState(false);
  const none = intl.formatMessage({ id: 'jobs.none' });
  const logs = logsLink(GRAFANA_URL, d.explore.logsQuery, r, serverNow);
  const trace = traceLink(GRAFANA_URL, d.explore.traceId ?? r.traceId, r, serverNow);
  const retryable = may && scheduled && RETRYABLE.includes(r.status);
  const counts = Object.entries(r.summary ?? {});

  const close = () => {
    setAsking(false);
    retry.reset();
  };
  const send = (reason: string) =>
    retry.mutate(reason, {
      onSuccess: () => {
        step.done();
        close();
        toast.show({ message: intl.formatMessage({ id: 'jobs.done.retry' }) });
      },
      onError: (e) => step.ask(e, () => send(reason)),
    });

  return (
    <>
      <section className="tz-card" aria-labelledby={`run-${r.id}`}>
        <div className={css.stack}>
          <div className={css.head}>
            <h2 id={`run-${r.id}`} className="tz-card-title">
              {intl.formatMessage({ id: 'jobs.run.title' })}
            </h2>
            <StatusPill tone={labels.statusTone(r.status)}>{labels.status(r.status)}</StatusPill>
          </div>
          <dl className={css.facts}>
            <dt>{intl.formatMessage({ id: 'jobs.run.id' })}</dt>
            <dd className={own.mono}>{r.id}</dd>
            <dt>{intl.formatMessage({ id: 'jobs.run.trigger' })}</dt>
            <dd>{labels.trigger(r.trigger)}</dd>
            <dt>{intl.formatMessage({ id: 'jobs.run.period' })}</dt>
            <dd>{r.periodKey ?? none}</dd>
            <dt>{intl.formatMessage({ id: 'jobs.run.started' })}</dt>
            <dd>{r.startedAt ? when(r.startedAt, serverNow) : none}</dd>
            <dt>{intl.formatMessage({ id: 'jobs.run.duration' })}</dt>
            <dd>{labels.duration(r.durationMs)}</dd>
            <dt>{intl.formatMessage({ id: 'jobs.run.late' })}</dt>
            <dd>{labels.duration(r.latenessMs)}</dd>
            {r.triggeredBy && (
              <>
                <dt>{intl.formatMessage({ id: 'jobs.run.by' })}</dt>
                <dd className={own.mono}>{r.triggeredBy}</dd>
              </>
            )}
          </dl>
          {counts.length > 0 && (
            <div>
              <h3 className="tz-field-label">{intl.formatMessage({ id: 'jobs.run.summary' })}</h3>
              <ul className={own.counts}>
                {counts.map(([k, n]) => (
                  <li key={k}>
                    <StatusPill tone="neutral">{`${k} ${intl.formatNumber(n)}`}</StatusPill>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {d.note && (
            <div>
              <h3 className="tz-field-label">{intl.formatMessage({ id: 'jobs.run.note' })}</h3>
              <p>{d.note}</p>
            </div>
          )}
          {d.error && (
            <div>
              <h3 className="tz-field-label">{intl.formatMessage({ id: 'jobs.run.error' })}</h3>
              <pre className={own.code}>{d.error}</pre>
            </div>
          )}
          {d.attempts.length > 0 && (
            <div>
              <h3 className="tz-field-label">{intl.formatMessage({ id: 'jobs.run.attempts' })}</h3>
              <ol className={own.timeline}>
                {d.attempts.map((a) => (
                  <li key={a.id}>
                    <span className={own.inline}>
                      <strong>{intl.formatMessage({ id: 'jobs.run.attempt' }, { n: a.attempt })}</strong>
                      <StatusPill tone={labels.statusTone(a.status)}>{labels.status(a.status)}</StatusPill>
                      <span className={css.muted}>{a.startedAt ? when(a.startedAt, serverNow) : none}</span>
                    </span>
                    {a.error && <pre className={own.code}>{a.error}</pre>}
                  </li>
                ))}
              </ol>
            </div>
          )}
          <div className={css.actions}>
            {logs && (
              <a href={logs} target="_blank" rel="noreferrer" className="tz-btn tz-btn-ghost tz-btn-sm">
                {intl.formatMessage({ id: 'jobs.run.logs' })}
              </a>
            )}
            {trace && (
              <a href={trace} target="_blank" rel="noreferrer" className="tz-btn tz-btn-ghost tz-btn-sm">
                {intl.formatMessage({ id: 'jobs.run.trace' })}
              </a>
            )}
            {retryable && (
              <Button size="sm" variant="soft" onClick={() => setAsking(true)}>
                {intl.formatMessage({ id: 'jobs.act.retry' })}
              </Button>
            )}
          </div>
          {!logs && <p className={css.muted}>{intl.formatMessage({ id: 'jobs.run.noGrafana' })}</p>}
        </div>
      </section>
      {may && <AuditPanel type="job_run" id={r.id} />}
      <ReasonDialog
        open={asking}
        title={intl.formatMessage({ id: 'jobs.act.retry.title' })}
        description={intl.formatMessage({ id: 'jobs.act.retry.body' })}
        confirmLabel={intl.formatMessage({ id: 'jobs.act.retry' })}
        pending={retry.isPending}
        error={retry.error && !(retry.error instanceof ApiError && retry.error.code === 'step_up_required') ? jobProblem(retry.error) : null}
        onClose={close}
        onConfirm={send}
      />
      {step.dialog}
    </>
  );
}
