import type { AdminSystem } from '@todayz/api-client/gen/model/adminSystem';
import type { AdminSystemAlarmState } from '@todayz/api-client/gen/model/adminSystemAlarmState';
import type { AdminSystemHealth } from '@todayz/api-client/gen/model/adminSystemHealth';
import type { AdminSystemPanelStatus } from '@todayz/api-client/gen/model/adminSystemPanelStatus';
import type { AdminSystemQueue } from '@todayz/api-client/gen/model/adminSystemQueue';
import type { AdminSystemStatement } from '@todayz/api-client/gen/model/adminSystemStatement';
import type { AdminSystemStatementsStatus } from '@todayz/api-client/gen/model/adminSystemStatementsStatus';
import { Button, DataTable, Skeleton, StatusPill, type ColumnDef, type PillTone } from '@todayz/ui';
import { useMemo, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { GRAFANA_URL } from '../app/env';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { useSystem } from './queries';
import css from '../app/page.module.css';
import own from './system.module.css';

const HEALTH_TONE: Readonly<Record<AdminSystemHealth, PillTone>> = { green: 'completed', yellow: 'pending', red: 'rejected', grey: 'neutral', unknown: 'neutral' };
const HEALTH: Readonly<Record<AdminSystemHealth, string>> = {
  green: 'system.health.green',
  yellow: 'system.health.yellow',
  red: 'system.health.red',
  grey: 'system.health.grey',
  unknown: 'system.health.unknown',
};
const ALARM_TONE: Readonly<Record<AdminSystemAlarmState, PillTone>> = { ok: 'completed', alarm: 'rejected', insufficient_data: 'neutral' };
const ALARM: Readonly<Record<AdminSystemAlarmState, string>> = {
  ok: 'system.alarm.ok',
  alarm: 'system.alarm.alarm',
  insufficient_data: 'system.alarm.insufficient',
};
const STATEMENTS: Readonly<Record<Exclude<AdminSystemStatementsStatus, 'ok'>, string>> = {
  not_installed: 'system.db.notInstalled',
  not_preloaded: 'system.db.notPreloaded',
  error: 'system.db.statementsError',
};

/** The dashboards an on-call admin opens first (infra/grafana/dashboards uids). */
const DASHBOARDS: readonly { uid: string; label: string }[] = [
  { uid: 'todayz-00', label: 'system.dash.fix' },
  { uid: 'todayz-01', label: 'system.dash.service' },
  { uid: 'todayz-02', label: 'system.dash.api' },
  { uid: 'todayz-03', label: 'system.dash.database' },
  { uid: 'tz-jobs', label: 'system.dash.jobs' },
  { uid: 'todayz-12', label: 'system.dash.security' },
];

/**
 * A-22 System health, read-only (admin and super_admin): Beanstalk, the load balancer, RDS and
 * CloudWatch alarms from AWS, the database and River queues from the API's own pool, and Grafana
 * links. Each panel says how old its data is; locally the AWS panels say "Not configured (local)".
 */
export function SystemPage() {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useSystem();
  return (
    <div className={css.column}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.system' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'system.sub' })}</p>
        </div>
        <Button variant="soft" size="sm" loading={q.isFetching} onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'system.refresh' })}
        </Button>
      </div>
      {q.isPending ? (
        <Skeleton shape="block" height="480px" />
      ) : q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <Panels d={q.data} />
      )}
    </div>
  );
}

function Panels({ d }: { d: AdminSystem }) {
  const intl = useIntl();
  const when = useWhen();
  const none = intl.formatMessage({ id: 'system.none' });
  const now = d.serverNow;
  const num = (n: number | null | undefined) => (n === null || n === undefined ? none : intl.formatNumber(n));
  const pct = (bp: number | null | undefined) => (bp === null || bp === undefined ? none : intl.formatNumber(bp / 10_000, { style: 'percent', maximumFractionDigits: 2 }));
  const ms = (n: number | null | undefined) => (n === null || n === undefined ? none : intl.formatMessage({ id: 'system.ms' }, { n: Math.round(n) }));
  const gb = (bytes: number | null | undefined) =>
    bytes === null || bytes === undefined ? none : intl.formatMessage({ id: 'system.gb' }, { n: intl.formatNumber(bytes / 1024 ** 3, { maximumFractionDigits: 1 }) });
  const multiAz = (on: boolean) => intl.formatMessage({ id: on ? 'system.multiAz.on' : 'system.multiAz.off' });

  return (
    <div className={own.grid}>
      <Panel id="sys-beanstalk" title={intl.formatMessage({ id: 'system.beanstalk' })} status={d.beanstalk.status} errorClass={d.beanstalk.errorClass} fetchedAt={d.beanstalk.fetchedAt} now={now}>
        {d.beanstalk.environments.length === 0 ? (
          <p className={css.muted}>{none}</p>
        ) : (
          d.beanstalk.environments.map((e) => (
            <div key={e.name} className={css.stack}>
              <div className={css.title}>
                <strong>{e.name}</strong>
                <StatusPill tone={HEALTH_TONE[e.health]}>{intl.formatMessage({ id: HEALTH[e.health] })}</StatusPill>
              </div>
              <dl className={css.facts}>
                <dt>{intl.formatMessage({ id: 'system.eb.status' })}</dt>
                <dd>{e.status}</dd>
                <dt>{intl.formatMessage({ id: 'system.eb.version' })}</dt>
                <dd className={own.mono}>{e.versionLabel ?? none}</dd>
                <dt>{intl.formatMessage({ id: 'system.eb.instances' })}</dt>
                <dd>{num(e.instanceCount)}</dd>
                <dt>{intl.formatMessage({ id: 'system.eb.updated' })}</dt>
                <dd>{e.updatedAt ? when(e.updatedAt, now) : none}</dd>
              </dl>
              {e.deploys.length > 0 && (
                <div>
                  <h3 className="tz-field-label">{intl.formatMessage({ id: 'system.eb.deploys' })}</h3>
                  <ol className={own.list}>
                    {e.deploys.map((x) => (
                      <li key={`${x.at}-${x.versionLabel}`} className={own.row}>
                        <span className={own.mono}>{x.versionLabel}</span>
                        <StatusPill tone={x.outcome === 'succeeded' ? 'completed' : 'rejected'}>
                          {intl.formatMessage({ id: x.outcome === 'succeeded' ? 'system.deploy.succeeded' : 'system.deploy.failed' })}
                        </StatusPill>
                        <span className={css.muted}>{when(x.at, now)}</span>
                      </li>
                    ))}
                  </ol>
                </div>
              )}
            </div>
          ))
        )}
      </Panel>

      <Panel id="sys-alb" title={intl.formatMessage({ id: 'system.alb' })} status={d.alb.status} errorClass={d.alb.errorClass} fetchedAt={d.alb.fetchedAt} now={now}>
        <p className={css.muted}>{intl.formatMessage({ id: 'system.alb.window' }, { n: d.alb.windowMinutes })}</p>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'system.alb.rpm' })}</dt>
          <dd>{num(d.alb.requestsPerMinute)}</dd>
          <dt>{intl.formatMessage({ id: 'system.alb.5xx' })}</dt>
          <dd>{pct(d.alb.http5xxRateBp)}</dd>
          <dt>{intl.formatMessage({ id: 'system.alb.p95' })}</dt>
          <dd>{ms(d.alb.p95Ms)}</dd>
        </dl>
      </Panel>

      <Panel id="sys-rds" title={intl.formatMessage({ id: 'system.rds' })} status={d.rds.status} errorClass={d.rds.errorClass} fetchedAt={d.rds.fetchedAt} now={now}>
        {d.rds.instances.length === 0 ? (
          <p className={css.muted}>{none}</p>
        ) : (
          d.rds.instances.map((i) => (
            <div key={i.name} className={css.stack}>
              <strong>{i.name}</strong>
              <dl className={css.facts}>
                <dt>{intl.formatMessage({ id: 'system.rds.status' })}</dt>
                <dd>{i.status}</dd>
                <dt>{intl.formatMessage({ id: 'system.rds.class' })}</dt>
                <dd className={own.mono}>
                  {i.instanceClass} · {i.engineVersion}
                </dd>
                <dt>{intl.formatMessage({ id: 'system.rds.cpu' })}</dt>
                <dd>{i.cpuPercent === null ? none : intl.formatNumber(i.cpuPercent / 100, { style: 'percent', maximumFractionDigits: 1 })}</dd>
                <dt>{intl.formatMessage({ id: 'system.rds.connections' })}</dt>
                <dd>{num(i.connections)}</dd>
                <dt>{intl.formatMessage({ id: 'system.rds.free' })}</dt>
                <dd>
                  {gb(i.freeStorageBytes)} / {intl.formatMessage({ id: 'system.gb' }, { n: intl.formatNumber(i.allocatedStorageGb) })}
                </dd>
                <dt>{intl.formatMessage({ id: 'system.multiAz' })}</dt>
                <dd>{multiAz(i.multiAz)}</dd>
              </dl>
            </div>
          ))
        )}
      </Panel>

      <Panel id="sys-alarms" title={intl.formatMessage({ id: 'system.alarms' })} status={d.alarms.status} errorClass={d.alarms.errorClass} fetchedAt={d.alarms.fetchedAt} now={now}>
        {d.alarms.items.length === 0 ? (
          <p className={css.muted}>{intl.formatMessage({ id: 'system.alarms.none' })}</p>
        ) : (
          <ul className={own.list}>
            {d.alarms.items.map((a) => (
              <li key={a.name} className={own.row}>
                <StatusPill tone={ALARM_TONE[a.state]}>{intl.formatMessage({ id: ALARM[a.state] })}</StatusPill>
                <span className={own.mono}>{a.name}</span>
                <span className={css.muted}>{a.updatedAt ? when(a.updatedAt, now) : none}</span>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel id="sys-river" title={intl.formatMessage({ id: 'system.river' })} status={d.river.status} errorClass={d.river.errorClass} fetchedAt={d.river.fetchedAt} now={now}>
        <p>{intl.formatMessage({ id: 'system.river.running' }, { n: d.river.running ?? 0 })}</p>
        <Queues rows={d.river.queues} />
      </Panel>

      <Panel
        id="sys-db"
        wide
        title={intl.formatMessage({ id: 'system.db' })}
        status={d.database.status}
        errorClass={d.database.errorClass}
        fetchedAt={d.database.fetchedAt}
        now={now}
      >
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'system.db.connections' })}</dt>
          <dd>
            {num(d.database.connections)} / {num(d.database.maxConnections)}
          </dd>
          <dt>{intl.formatMessage({ id: 'system.db.size' })}</dt>
          <dd>{gb(d.database.sizeBytes)}</dd>
          {d.database.multiAz !== null && (
            <>
              <dt>{intl.formatMessage({ id: 'system.multiAz' })}</dt>
              <dd>{multiAz(d.database.multiAz)}</dd>
            </>
          )}
        </dl>
        {d.database.statementsStatus === 'ok' ? (
          <Statements rows={d.database.statements} />
        ) : (
          <p className={css.muted}>{intl.formatMessage({ id: STATEMENTS[d.database.statementsStatus] })}</p>
        )}
      </Panel>

      <section className={`tz-card ${own.wide}`} aria-labelledby="sys-grafana">
        <h2 id="sys-grafana" className="tz-card-title">
          {intl.formatMessage({ id: 'system.grafana' })}
        </h2>
        {GRAFANA_URL ? (
          <ul className={own.links}>
            {DASHBOARDS.map((x) => (
              <li key={x.uid}>
                <a href={`${GRAFANA_URL}/d/${x.uid}`} target="_blank" rel="noreferrer" className="tz-btn tz-btn-soft tz-btn-sm">
                  {intl.formatMessage({ id: x.label })}
                </a>
              </li>
            ))}
          </ul>
        ) : (
          <p className={css.muted}>{intl.formatMessage({ id: 'system.grafana.none' })}</p>
        )}
      </section>
    </div>
  );
}

/** One panel: its title, how old the data is, and its content, or why there is none. */
function Panel(props: {
  id: string;
  title: string;
  status: AdminSystemPanelStatus;
  errorClass: string | null;
  fetchedAt: string;
  now: string;
  wide?: boolean;
  children: ReactNode;
}) {
  const { id, title, status, errorClass, fetchedAt, now, wide, children } = props;
  const intl = useIntl();
  const age = Math.max(0, Math.round((Date.parse(now) - Date.parse(fetchedAt)) / 1000));
  return (
    <section className={`tz-card ${wide ? own.wide : ''}`} aria-labelledby={id}>
      <div className={css.stack}>
        <div className={css.head}>
          <h2 id={id} className="tz-card-title">
            {title}
          </h2>
          {status !== 'not_configured' && (
            <span className={css.muted}>
              {age < 60 ? intl.formatMessage({ id: 'system.age.s' }, { n: age }) : intl.formatMessage({ id: 'system.age.min' }, { n: Math.floor(age / 60) })}
            </span>
          )}
        </div>
        {status === 'not_configured' ? (
          <p className={css.muted}>{intl.formatMessage({ id: 'system.notConfigured' })}</p>
        ) : status === 'error' ? (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'system.error' })}
            {errorClass && <span className={own.mono}> ({errorClass})</span>}
          </p>
        ) : (
          children
        )}
      </div>
    </section>
  );
}

function Queues({ rows }: { rows: AdminSystemQueue[] }) {
  const intl = useIntl();
  const columns = useMemo<ColumnDef<AdminSystemQueue, unknown>[]>(
    () => [
      { id: 'queue', header: intl.formatMessage({ id: 'system.river.queue' }), accessorFn: (r) => r.queue },
      { id: 'depth', header: intl.formatMessage({ id: 'system.river.depth' }), accessorFn: (r) => intl.formatNumber(r.depth), meta: { numeric: true } },
      {
        id: 'oldest',
        header: intl.formatMessage({ id: 'system.river.oldest' }),
        accessorFn: (r) => intl.formatMessage({ id: 'system.age.s' }, { n: r.oldestAgeSeconds }),
        meta: { numeric: true },
      },
    ],
    [intl],
  );
  return (
    <DataTable
      label={intl.formatMessage({ id: 'system.river.table' })}
      columns={columns}
      data={rows}
      getRowId={(r) => r.queue}
      emptyLabel={intl.formatMessage({ id: 'system.river.empty' })}
    />
  );
}

/** A statement with its rank: two roles can share the same normalised text. */
type Ranked = AdminSystemStatement & { rank: number };

/** The top SQL statements by total time (pg_stat_statements; DML only, text normalised by Postgres). */
function Statements({ rows }: { rows: AdminSystemStatement[] }) {
  const intl = useIntl();
  const data = useMemo(() => rows.map((r, rank) => ({ ...r, rank })), [rows]);
  const columns = useMemo<ColumnDef<Ranked, unknown>[]>(
    () => [
      { id: 'query', header: intl.formatMessage({ id: 'system.sql.query' }), cell: ({ row }) => <code className={own.sql}>{row.original.query}</code> },
      { id: 'calls', header: intl.formatMessage({ id: 'system.sql.calls' }), accessorFn: (r) => intl.formatNumber(r.calls), meta: { numeric: true } },
      {
        id: 'mean',
        header: intl.formatMessage({ id: 'system.sql.mean' }),
        accessorFn: (r) => intl.formatMessage({ id: 'system.ms' }, { n: intl.formatNumber(r.meanMs, { maximumFractionDigits: 1 }) }),
        meta: { numeric: true },
      },
      {
        id: 'total',
        header: intl.formatMessage({ id: 'system.sql.total' }),
        accessorFn: (r) => intl.formatMessage({ id: 'system.ms' }, { n: intl.formatNumber(Math.round(r.totalMs)) }),
        meta: { numeric: true },
      },
      { id: 'rows', header: intl.formatMessage({ id: 'system.sql.rows' }), accessorFn: (r) => intl.formatNumber(r.rows), meta: { numeric: true } },
    ],
    [intl],
  );
  return (
    <DataTable
      label={intl.formatMessage({ id: 'system.sql.table' })}
      columns={columns}
      data={data}
      getRowId={(r) => String(r.rank)}
      emptyLabel={intl.formatMessage({ id: 'system.sql.empty' })}
    />
  );
}
