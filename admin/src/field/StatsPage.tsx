import type { FieldAgentStats } from '@todayz/api-client/gen/model/fieldAgentStats';
import type { FieldWeek } from '@todayz/api-client/gen/model/fieldWeek';
import { Button, DataTable, EmptyState, Skeleton, type ColumnDef } from '@todayz/ui';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { useFieldLabels } from './labels';
import { useAgents, useMyStats } from './queries';
import css from './field.module.css';

/**
 * A-15. A field agent sees only their own numbers (`/field/me/stats`); admins see every agent and
 * pick whose weekly trend to show (D-64).
 */
export function StatsPage() {
  const intl = useIntl();
  const all = can(useRoles(), 'fieldAgentsAll');
  return (
    <div className={`${css.page} ${all ? css.wide : ''}`}>
      <header>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.fieldAgents' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: all ? 'field.stats.subAll' : 'field.stats.subMine' })}</p>
      </header>
      {all ? <AllAgents /> : <Mine />}
    </div>
  );
}

function Failed({ error, retry }: { error: unknown; retry: () => void }) {
  const intl = useIntl();
  const problemText = useProblemText();
  return (
    <p role="alert" className="tz-field-error">
      {problemText(error)}{' '}
      <Button size="sm" variant="ghost" onClick={retry}>
        {intl.formatMessage({ id: 'common.retry' })}
      </Button>
    </p>
  );
}

function Mine() {
  const q = useMyStats(true);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) return <Failed error={q.error} retry={() => void q.refetch()} />;
  return <AgentCard stats={q.data.stats} />;
}

function AllAgents() {
  const intl = useIntl();
  const { percent } = useFieldLabels();
  const q = useAgents(true);
  const [picked, setPicked] = useState<string | null>(null);
  const columns = useMemo<ColumnDef<FieldAgentStats, unknown>[]>(
    () => [
      { id: 'name', header: intl.formatMessage({ id: 'field.stats.col.agent' }), accessorFn: (a) => a.name ?? '—' },
      { id: 'onboarded', header: intl.formatMessage({ id: 'field.stats.onboarded' }), accessorFn: (a) => a.businesses.total },
      { id: 'verified', header: intl.formatMessage({ id: 'field.stats.verified' }), accessorFn: (a) => a.businesses.verified },
      { id: 'live', header: intl.formatMessage({ id: 'field.stats.live' }), accessorFn: (a) => a.storesLive },
      { id: 'items', header: intl.formatMessage({ id: 'field.stats.items' }), accessorFn: (a) => a.itemsAdded },
      { id: 'window', header: intl.formatMessage({ id: 'field.stats.inWindow' }), accessorFn: (a) => a.inWindow },
      {
        id: 'firstOrder',
        header: intl.formatMessage({ id: 'field.stats.firstOrder' }),
        accessorFn: (a) => a.firstOrderRate ?? -1,
        cell: ({ row }) => percent(row.original.firstOrderRate),
      },
      {
        id: 'trend',
        header: intl.formatMessage({ id: 'field.stats.col.trend' }),
        enableSorting: false,
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={picked === row.original.agentId}
            onClick={() => setPicked(row.original.agentId)}
            aria-label={intl.formatMessage({ id: 'field.stats.showTrend' }, { name: row.original.name ?? '—' })}
          >
            {intl.formatMessage({ id: 'field.stats.trend' })}
          </Button>
        ),
      },
    ],
    [intl, percent, picked],
  );
  if (q.isError) return <Failed error={q.error} retry={() => void q.refetch()} />;
  const items = q.data?.items ?? [];
  const shown = items.find((a) => a.agentId === picked) ?? items[0];
  return (
    <>
      <DataTable
        label={intl.formatMessage({ id: 'shell.nav.fieldAgents' })}
        columns={columns}
        data={items}
        getRowId={(a) => a.agentId}
        loading={q.isPending}
        emptyLabel={intl.formatMessage({ id: 'field.stats.none' })}
      />
      {shown && (
        <section className="tz-card" aria-label={intl.formatMessage({ id: 'field.stats.trendOf' }, { name: shown.name ?? '—' })}>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'field.stats.trendOf' }, { name: shown.name ?? '—' })}</h2>
          <Weekly weeks={shown.weekly} />
        </section>
      )}
    </>
  );
}

function AgentCard({ stats: s }: { stats: FieldAgentStats }) {
  const intl = useIntl();
  const { percent } = useFieldLabels();
  if (s.businesses.total === 0)
    return (
      <EmptyState
        title={intl.formatMessage({ id: 'field.stats.empty.title' })}
        description={intl.formatMessage({ id: 'field.stats.empty.body' })}
      />
    );
  const tiles: [string, string][] = [
    ['field.stats.onboarded', intl.formatNumber(s.businesses.total)],
    ['field.stats.verified', intl.formatNumber(s.businesses.verified)],
    ['field.stats.live', intl.formatNumber(s.storesLive)],
    ['field.stats.items', intl.formatNumber(s.itemsAdded)],
    ['field.stats.inWindow', intl.formatNumber(s.inWindow)],
    ['field.stats.firstOrder', percent(s.firstOrderRate)],
  ];
  return (
    <>
      <ul className={`${css.tiles} ${css.list}`} aria-label={intl.formatMessage({ id: 'field.stats.mine' })}>
        {tiles.map(([label, value]) => (
          <li key={label} className={css.tile}>
            <span className={css.muted}>{intl.formatMessage({ id: label })}</span>
            <span className={css.tileValue}>{value}</span>
          </li>
        ))}
      </ul>
      <section className="tz-card">
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'field.stats.weekly' })}</h2>
        <Weekly weeks={s.weekly} />
      </section>
    </>
  );
}

/** Businesses onboarded per week, oldest first; each bar names its numbers for screen readers. */
function Weekly({ weeks }: { weeks: FieldWeek[] }) {
  const intl = useIntl();
  const top = Math.max(1, ...weeks.map((w) => w.onboarded));
  return (
    <>
      <ol className={css.chart} aria-label={intl.formatMessage({ id: 'field.stats.weekly' })}>
        {weeks.map((w) => {
          const label = intl.formatDate(w.weekStart, { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' });
          return (
            <li
              key={w.weekStart}
              className={css.week}
              aria-label={intl.formatMessage(
                { id: 'field.stats.week' },
                { week: label, onboarded: w.onboarded, verified: w.verified, items: w.itemsAdded },
              )}
            >
              <span className={css.muted} aria-hidden="true">
                {w.onboarded}
              </span>
              <span className={css.bar} style={{ height: `${(w.onboarded / top) * 100}%` }} aria-hidden="true" />
              <span className={css.barLabel} aria-hidden="true">
                {label}
              </span>
            </li>
          );
        })}
      </ol>
      <p className={css.muted}>{intl.formatMessage({ id: 'field.stats.weekly.help' })}</p>
    </>
  );
}
