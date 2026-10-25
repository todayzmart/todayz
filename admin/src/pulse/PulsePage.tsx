import type { AdminPulse } from '@todayz/api-client/gen/model/adminPulse';
import type { AdminPulseAttention } from '@todayz/api-client/gen/model/adminPulseAttention';
import type { AdminPulseKpi } from '@todayz/api-client/gen/model/adminPulseKpi';
import type { AdminPulseRange } from '@todayz/api-client/gen/model/adminPulseRange';
import type { AdminPulseRateKpi } from '@todayz/api-client/gen/model/adminPulseRateKpi';
import { Button, DataTable, KpiStrip, Segmented, StatusPill, type ColumnDef, type KpiDelta, type KpiProps } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { Activity, IndianRupee, Search, ShoppingBag, Sparkles, Store, Users, UserRoundCheck } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { canOpen, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { cityRows, countChange, dayLabel, type CityRow, funnelSteps, percent, rateChange, TREND, trendModel, type TrendMetric } from './model';
import { ATTENTION_LABEL, CREDITS_LABEL, DELTA_LABEL, FUNNEL_LABEL, KPI_LABEL, LEAK_LABEL, METRIC_LABEL, RANGE_LABEL, RETENTION_LABEL, VS_LABEL } from './labels';
import { usePulse, usePulseMap } from './queries';
import type { PulseSearch } from './search';
import page from '../app/page.module.css';
import css from './pulse.module.css';

const RANGES: readonly AdminPulseRange[] = ['today', '7d', '30d', '90d'];
const nf = new Intl.NumberFormat('en-IN');
const count = (n: number) => nf.format(n);

/** A-02 Pulse: platform-wide numbers from the rollups that Grafana 07–11 read, so the two agree within rounding. Read-only. */
export function PulsePage({ search }: { search: PulseSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const when = useWhen();
  const problemText = useProblemText();
  const range: AdminPulseRange = search.range ?? '7d';
  const q = usePulse(range, canOpen(useRoles(), ''));
  const p = q.data;

  return (
    <div className={page.column}>
      <div className={page.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.pulse' })}</h1>
          <p className="tz-page-sub">
            {intl.formatMessage({ id: 'pulse.sub' })}{' '}
            {p &&
              (p.asOf
                ? intl.formatMessage({ id: 'pulse.asOf' }, { when: when(p.asOf, p.serverNow) })
                : intl.formatMessage({ id: 'pulse.asOf.never' }))}
          </p>
        </div>
        <Segmented
          label={intl.formatMessage({ id: 'pulse.range' })}
          options={RANGES.map((r) => ({ value: r, label: intl.formatMessage({ id: RANGE_LABEL[r] }) }))}
          value={range}
          onChange={(r) => void navigate({ to: '/', search: r === '7d' ? {} : { range: r } })}
        />
      </div>
      {q.isError && !p ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <Body p={p} range={range} />
      )}
    </div>
  );
}

function Body({ p, range }: { p: AdminPulse | undefined; range: AdminPulseRange }) {
  const intl = useIntl();
  const loading = !p;
  const vs = intl.formatMessage({ id: VS_LABEL[range] });
  const k = p?.kpis;

  const ofCount = (kpi: AdminPulseKpi | undefined): KpiDelta | undefined => {
    const c = kpi && countChange(kpi);
    return c && { value: `${c.percent}%`, direction: c.direction, label: intl.formatMessage({ id: DELTA_LABEL[c.direction] }, { change: `${c.percent}%` }) };
  };
  const ofRate = (kpi: AdminPulseRateKpi | undefined): KpiDelta | undefined => {
    const c = kpi && rateChange(kpi);
    if (!c) return undefined;
    const change = intl.formatMessage({ id: 'pulse.points' }, { n: intl.formatNumber(c.points) });
    return { value: change, direction: c.direction, label: intl.formatMessage({ id: DELTA_LABEL[c.direction] }, { change }) };
  };
  const tile = (key: keyof typeof KPI_LABEL, icon: KpiProps['icon'], value: string | undefined, delta: KpiDelta | undefined, note?: string) => ({
    key,
    icon,
    name: intl.formatMessage({ id: KPI_LABEL[key] }),
    value: value ?? '',
    loading,
    delta,
    comparison: delta ? vs : undefined,
    note,
  });

  return (
    <>
      <KpiStrip
        label={intl.formatMessage({ id: 'pulse.kpis.trade' })}
        items={[
          tile(
            'gmv',
            IndianRupee,
            k && money(k.gmvPaise.value),
            ofCount(k?.gmvPaise),
            k && intl.formatMessage({ id: 'pulse.kpi.gmv.note' }, { online: money(k.onlineGmvPaise.value), pos: money(k.posGmvPaise.value) }),
          ),
          tile('orders', ShoppingBag, k && count(k.orders.value), ofCount(k?.orders), k && intl.formatMessage({ id: 'pulse.kpi.orders.note' }, { n: count(k.completedOrders.value) })),
          tile('success', Activity, k && percent(k.successRateBp.value), ofRate(k?.successRateBp)),
          tile(
            'customers',
            Users,
            k && count(k.activeCustomers.value),
            ofCount(k?.activeCustomers),
            k && intl.formatMessage({ id: 'pulse.kpi.customers.note' }, { n: count(k.transactingCustomers.value) }),
          ),
        ]}
      />
      <KpiStrip
        label={intl.formatMessage({ id: 'pulse.kpis.supply' })}
        items={[
          tile('live', Store, k && count(k.liveStores.value), ofCount(k?.liveStores), k && intl.formatMessage({ id: 'pulse.kpi.live.note' }, { n: count(k.activeStores.value) })),
          tile('new', Sparkles, k && count(k.newStores.value), ofCount(k?.newStores)),
          tile(
            'search',
            Search,
            k && percent(k.searchSuccessRateBp.value),
            ofRate(k?.searchSuccessRateBp),
            k && intl.formatMessage({ id: 'pulse.kpi.search.note' }, { n: count(k.searches.value) }),
          ),
          tile('d7', UserRoundCheck, k && percent(k.d7RetentionBp.value), ofRate(k?.d7RetentionBp)),
        ]}
      />
      {p && (
        <>
          <Trend p={p} />
          <div className={css.pair}>
            <Funnel p={p} />
            <Leaks p={p} />
          </div>
          <Retention p={p} />
          <div className={css.pair}>
            <ZeroResults p={p} />
            <Credits p={p} />
          </div>
          <Attention p={p} />
          <Cities range={range} />
        </>
      )}
    </>
  );
}

const METRICS: readonly TrendMetric[] = ['gmvPaise', 'orders', 'activeCustomers', 'searches'];

function Trend({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  const [metric, setMetric] = useState<TrendMetric>('gmvPaise');
  const m = useMemo(() => trendModel(p.trend, metric), [p.trend, metric]);
  const fmt = (v: number) => (metric === 'gmvPaise' ? money(v) : count(v));
  const name = intl.formatMessage({ id: METRIC_LABEL[metric] });
  return (
    <section className="tz-card" aria-labelledby="pulse-trend">
      <div className={page.head}>
        <h2 id="pulse-trend" className="tz-card-title">
          {intl.formatMessage({ id: 'pulse.trend' })}
        </h2>
        <Segmented
          label={intl.formatMessage({ id: 'pulse.trend.metric' })}
          options={METRICS.map((v) => ({ value: v, label: intl.formatMessage({ id: METRIC_LABEL[v] }) }))}
          value={metric}
          onChange={setMetric}
        />
      </div>
      <figure className={css.chart}>
        <svg viewBox={`0 0 ${TREND.width} ${TREND.height}`} role="img" aria-label={intl.formatMessage({ id: 'pulse.trend.label' }, { metric: name, from: dayLabel(p.from), to: dayLabel(p.to), max: fmt(m.max) })}>
          <line className={css.axis} x1={TREND.left} x2={TREND.right} y1={TREND.bottom} y2={TREND.bottom} />
          {m.bars.map((b, i) => (
            <g key={b.date}>
              <rect className={i === m.bars.length - 1 ? css.barNow : css.bar} x={b.x} y={b.y} width={b.width} height={b.height} rx={2}>
                <title>{`${dayLabel(b.date)}: ${fmt(b.value)}`}</title>
              </rect>
              {m.labelled.has(i) && (
                <text className={css.tick} x={b.x + b.width / 2} y={TREND.height - 8} textAnchor="middle">
                  {dayLabel(b.date)}
                </text>
              )}
            </g>
          ))}
        </svg>
        <figcaption className={page.muted}>{intl.formatMessage({ id: 'pulse.trend.peak' }, { metric: name, max: fmt(m.max) })}</figcaption>
      </figure>
    </section>
  );
}

function Funnel({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-labelledby="pulse-funnel">
      <h2 id="pulse-funnel" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.funnel' })}
      </h2>
      <ol className={css.funnel}>
        {funnelSteps(p.funnel).map((s) => (
          <li key={s.key}>
            <div className={css.funnelRow}>
              <span>{intl.formatMessage({ id: FUNNEL_LABEL[s.key] })}</span>
              <span className={css.num}>
                {count(s.count)}
                {s.ofPreviousBp !== null && <span className={page.muted}> · {percent(s.ofPreviousBp)}</span>}
              </span>
            </div>
            <div className={css.track} aria-hidden="true">
              <div className={css.fill} style={{ width: `${s.share}%` }} />
            </div>
          </li>
        ))}
      </ol>
      <p className={page.muted}>{intl.formatMessage({ id: 'pulse.funnel.note' })}</p>
    </section>
  );
}

function Leaks({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  const total = p.leaks.reduce((n, l) => n + l.count, 0);
  return (
    <section className="tz-card" aria-labelledby="pulse-leaks">
      <h2 id="pulse-leaks" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.leaks' })}
      </h2>
      {p.leaks.length === 0 ? (
        <p className={page.muted}>{intl.formatMessage({ id: 'pulse.leaks.none' })}</p>
      ) : (
        <ul className={css.list}>
          {p.leaks.map((l) => (
            <li key={`${l.status}-${l.reason ?? ''}`} className={css.funnelRow}>
              <span>
                {intl.formatMessage({ id: LEAK_LABEL[l.status] })}
                <span className={page.muted}> · {intl.formatMessage({ id: 'pulse.reason' }, { reason: l.reason ?? 'none' })}</span>
              </span>
              <span className={css.num}>
                {count(l.count)}
                <span className={page.muted}> · {percent(total > 0 ? Math.round((l.count / total) * 10000) : null)}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Retention({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  const r = p.retention;
  const weeks = Math.max(0, ...r.cohorts.map((c) => c.weeksBp.length));
  return (
    <section className="tz-card" aria-labelledby="pulse-retention">
      <h2 id="pulse-retention" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.retention' })}
      </h2>
      <dl className={css.figures}>
        {(['d1Bp', 'd7Bp', 'd30Bp'] as const).map((key) => (
          <div key={key}>
            <dt>{intl.formatMessage({ id: RETENTION_LABEL[key] })}</dt>
            <dd className={css.figure}>{percent(r[key])}</dd>
          </div>
        ))}
      </dl>
      {r.cohorts.length === 0 ? (
        <p className={page.muted}>{intl.formatMessage({ id: 'pulse.retention.none' })}</p>
      ) : (
        <div className={css.scroll}>
          <table className={css.cohorts}>
            <caption className={page.muted}>{intl.formatMessage({ id: 'pulse.retention.cohorts' })}</caption>
            <thead>
              <tr>
                <th scope="col">{intl.formatMessage({ id: 'pulse.retention.week' })}</th>
                <th scope="col">{intl.formatMessage({ id: 'pulse.retention.size' })}</th>
                {Array.from({ length: weeks }, (_, w) => (
                  <th key={w} scope="col">
                    {intl.formatMessage({ id: 'pulse.retention.w' }, { n: w })}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {r.cohorts.map((c) => (
                <tr key={c.cohortWeek}>
                  <th scope="row">{dayLabel(c.cohortWeek)}</th>
                  <td className={css.num}>{count(c.size)}</td>
                  {Array.from({ length: weeks }, (_, w) => {
                    const bp = c.weeksBp[w];
                    return bp === undefined ? (
                      <td key={w} />
                    ) : (
                      <td key={w} className={css.heat} style={{ '--heat': `${Math.round(bp / 100)}%` } as React.CSSProperties}>
                        {percent(bp)}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function ZeroResults({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-labelledby="pulse-zero">
      <h2 id="pulse-zero" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.zero' })}
      </h2>
      {p.zeroResultSearches.length === 0 ? (
        <p className={page.muted}>{intl.formatMessage({ id: 'pulse.zero.none' })}</p>
      ) : (
        <ol className={css.list}>
          {p.zeroResultSearches.map((z) => (
            <li key={z.queryNorm} className={css.funnelRow}>
              <span>{z.queryNorm}</span>
              <span className={css.num}>{intl.formatMessage({ id: 'pulse.zero.count' }, { n: count(z.searches) })}</span>
            </li>
          ))}
        </ol>
      )}
      <p className={page.muted}>{intl.formatMessage({ id: 'pulse.zero.note' })}</p>
    </section>
  );
}

function Credits({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-labelledby="pulse-credits">
      <h2 id="pulse-credits" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.credits' })}
      </h2>
      <dl className={css.figures}>
        {(['issued', 'spent', 'expired'] as const).map((key) => (
          <div key={key}>
            <dt>{intl.formatMessage({ id: CREDITS_LABEL[key] })}</dt>
            <dd className={css.figure}>{count(p.credits[key])}</dd>
          </div>
        ))}
      </dl>
      <p className={page.muted}>{intl.formatMessage({ id: 'pulse.credits.note' })}</p>
    </section>
  );
}

function Attention({ p }: { p: AdminPulse }) {
  const intl = useIntl();
  const when = useWhen();
  const linked = canOpen(useRoles(), 'stores');
  const columns = useMemo<ColumnDef<AdminPulseAttention, unknown>[]>(
    () => [
      {
        id: 'store',
        header: intl.formatMessage({ id: 'pulse.attention.store' }),
        cell: ({ row }) => (
          <div>
            {linked ? (
              <Link to="/stores/$storeId" params={{ storeId: row.original.storeId }}>
                {row.original.name}
              </Link>
            ) : (
              row.original.name
            )}
            <div className={page.muted}>{row.original.city}</div>
          </div>
        ),
      },
      {
        id: 'reason',
        header: intl.formatMessage({ id: 'pulse.attention.reason' }),
        cell: ({ row }) => (
          <StatusPill tone={row.original.reason === 'not_accepting' ? 'cancelled' : 'pending'}>
            {intl.formatMessage({ id: ATTENTION_LABEL[row.original.reason] })}
          </StatusPill>
        ),
      },
      { id: 'expired', header: intl.formatMessage({ id: 'pulse.attention.expired' }), cell: ({ row }) => count(row.original.expiredUnaccepted) },
      { id: 'strikes', header: intl.formatMessage({ id: 'pulse.attention.strikes' }), cell: ({ row }) => count(row.original.strikes30d) },
      { id: 'accept', header: intl.formatMessage({ id: 'pulse.attention.accept' }), cell: ({ row }) => percent(row.original.acceptRateBp) },
      {
        id: 'stock',
        header: intl.formatMessage({ id: 'pulse.attention.stock' }),
        cell: ({ row }) => (row.original.stockUpdatedAt ? when(row.original.stockUpdatedAt, p.serverNow) : intl.formatMessage({ id: 'pulse.attention.stock.never' })),
      },
    ],
    [intl, when, linked, p.serverNow],
  );
  return (
    <section className={page.column} aria-labelledby="pulse-attention">
      <h2 id="pulse-attention" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.attention' })}
      </h2>
      <DataTable
        label={intl.formatMessage({ id: 'pulse.attention' })}
        columns={columns}
        data={p.storesNeedingAttention}
        getRowId={(s) => s.storeId}
        emptyLabel={intl.formatMessage({ id: 'pulse.attention.none' })}
      />
    </section>
  );
}

function Cities({ range }: { range: AdminPulseRange }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = usePulseMap(range, canOpen(useRoles(), ''));
  const rows = useMemo(() => (q.data ? cityRows(q.data) : []), [q.data]);
  const searches = q.data?.demand.reduce((n, c) => n + c.searches, 0) ?? 0;
  const columns = useMemo<ColumnDef<CityRow, unknown>[]>(
    () => [
      { id: 'city', header: intl.formatMessage({ id: 'pulse.cities.city' }), cell: ({ row }) => row.original.city },
      { id: 'stores', header: intl.formatMessage({ id: 'pulse.cities.stores' }), cell: ({ row }) => count(row.original.stores) },
      { id: 'live', header: intl.formatMessage({ id: 'pulse.cities.live' }), cell: ({ row }) => count(row.original.live) },
    ],
    [intl],
  );
  return (
    <section className={page.column} aria-labelledby="pulse-cities">
      <h2 id="pulse-cities" className="tz-card-title">
        {intl.formatMessage({ id: 'pulse.cities' })}
      </h2>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}
        </p>
      ) : (
        <>
          <DataTable
            label={intl.formatMessage({ id: 'pulse.cities' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.city}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'pulse.cities.none' })}
          />
          {q.data && (
            <p className={page.muted}>{intl.formatMessage({ id: 'pulse.cities.demand' }, { cells: q.data.demand.length, searches: count(searches) })}</p>
          )}
        </>
      )}
    </section>
  );
}
