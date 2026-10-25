import type { AnalyticsRange } from '@todayz/api-client/gen/model/analyticsRange';
import type { AnalyticsTopItem } from '@todayz/api-client/gen/model/analyticsTopItem';
import type { OrderFunnel } from '@todayz/api-client/gen/model/orderFunnel';
import type { StoreAnalytics } from '@todayz/api-client/gen/model/storeAnalytics';
import { formatMoney } from '@todayz/i18n';
import { Button, DataTable, EmptyState, KpiStrip, Segmented, Skeleton, VisuallyHidden, type ColumnDef } from '@todayz/ui';
import { Inbox } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useAnalytics } from './queries';
import styles from './analytics.module.css';

const RANGES: readonly AnalyticsRange[] = ['7d', '30d', '90d'];

/** An IST calendar day ("2026-01-10") at IST noon, so formatting never slips a day. */
const istNoon = (day: string) => new Date(`${day}T12:00:00+05:30`);
const dayShort = (intl: IntlShape, day: string) => intl.formatDate(istNoon(day), { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' });

function duration(intl: IntlShape, seconds: number): string {
  const s = Math.round(seconds);
  return s < 60
    ? intl.formatMessage({ id: 'analytics.duration.seconds' }, { s })
    : intl.formatMessage({ id: 'analytics.duration.minutes' }, { m: Math.floor(s / 60), s: s % 60 });
}

function hourLabel(intl: IntlShape, h: number): string {
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return h < 12 ? intl.formatMessage({ id: 'overview.hour.am' }, { h: h12 }) : intl.formatMessage({ id: 'overview.hour.pm' }, { h: h12 });
}

/** B-12 Sales: online against counter by day and hour, the order funnel, top items, mix, repeat rate, ratings. */
export function SalesTab({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const [range, setRange] = useState<AnalyticsRange>('7d');
  const q = useAnalytics(store, range);
  return (
    <div className={styles.stack}>
      <Segmented
        label={intl.formatMessage({ id: 'analytics.range' })}
        value={range}
        onChange={setRange}
        options={RANGES.map((r) => ({ value: r, label: intl.formatMessage({ id: `analytics.range.${r}` }) }))}
      />
      {q.isError ? (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'analytics.error' })}
          action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'overview.retry' })}</Button>}
        />
      ) : q.data ? (
        <Report data={q.data} />
      ) : (
        <div role="status" aria-label={intl.formatMessage({ id: 'analytics.loading' })} className={styles.stack}>
          <Skeleton shape="block" height="96px" />
          <Skeleton shape="block" height="200px" />
        </div>
      )}
    </div>
  );
}

function Report({ data }: { data: StoreAnalytics }) {
  const intl = useIntl();
  const daily = data.daily ?? [];
  const online = daily.reduce((n, d) => n + d.onlinePaise, 0);
  const pos = daily.reduce((n, d) => n + d.posPaise, 0);
  const bills = daily.reduce((n, d) => n + d.onlineBills + d.posBills, 0);
  return (
    <>
      <p className={styles.sub}>
        {intl.formatMessage({ id: 'analytics.span' }, { from: dayShort(intl, data.from), to: dayShort(intl, data.to) })}
      </p>
      <KpiStrip
        label={intl.formatMessage({ id: 'analytics.kpi.label' })}
        items={[
          { key: 'sales', name: intl.formatMessage({ id: 'analytics.kpi.sales' }), value: formatMoney(online + pos, 'dashboard') },
          { key: 'online', name: intl.formatMessage({ id: 'analytics.kpi.online' }), value: formatMoney(online, 'dashboard') },
          { key: 'pos', name: intl.formatMessage({ id: 'analytics.kpi.pos' }), value: formatMoney(pos, 'dashboard') },
          { key: 'bills', name: intl.formatMessage({ id: 'analytics.kpi.bills' }), value: intl.formatNumber(bills) },
        ]}
      />
      <div className={styles.grid}>
        <section className={`tz-card ${styles.wide}`}>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.daily.title' })}</h2>
          {online + pos === 0 ? (
            <p className={styles.note}>{intl.formatMessage({ id: 'analytics.daily.empty' })}</p>
          ) : (
            <Bars
              caption={intl.formatMessage({ id: 'analytics.daily.title' })}
              bars={daily.map((d) => ({ key: d.day, label: dayShort(intl, d.day), online: d.onlinePaise, pos: d.posPaise }))}
            />
          )}
        </section>
        <section className={`tz-card ${styles.wide}`}>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.hourly.title' })}</h2>
          {(data.byHour ?? []).every((h) => h.paise === 0) ? (
            <p className={styles.note}>{intl.formatMessage({ id: 'analytics.daily.empty' })}</p>
          ) : (
            <Bars
              caption={intl.formatMessage({ id: 'analytics.hourly.title' })}
              bars={(data.byHour ?? []).map((h) => ({ key: String(h.hour), label: hourLabel(intl, h.hour), total: h.paise }))}
            />
          )}
        </section>
        {data.funnel && <Funnel f={data.funnel} />}
        <TopItems items={data.topItems ?? []} />
        <CategoryMix mix={data.categoryMix ?? []} />
        <section className="tz-card">
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.repeat.title' })}</h2>
          {data.repeat && data.repeat.repeatPct !== null ? (
            <>
              <p className={styles.big}>{intl.formatNumber(data.repeat.repeatPct / 100, { style: 'percent' })}</p>
              <p className={styles.note}>
                {intl.formatMessage(
                  { id: 'analytics.repeat.note' },
                  { repeat: data.repeat.repeatCustomers, customers: data.repeat.customers },
                )}
              </p>
            </>
          ) : (
            <p className={styles.note}>{intl.formatMessage({ id: 'analytics.repeat.notEnough' })}</p>
          )}
        </section>
        <section className="tz-card">
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.ratings.title' })}</h2>
          {(data.ratings ?? []).length === 0 ? (
            <p className={styles.note}>{intl.formatMessage({ id: 'analytics.ratings.empty' })}</p>
          ) : (
            <ul className={styles.list}>
              {(data.ratings ?? []).map((w) => (
                <li key={w.week}>
                  <span>{intl.formatMessage({ id: 'analytics.ratings.week' }, { day: dayShort(intl, w.week) })}</span>
                  <span className="tz-num">
                    {intl.formatMessage(
                      { id: 'analytics.ratings.value' },
                      { stars: intl.formatNumber(w.avgStars, { maximumFractionDigits: 1, minimumFractionDigits: 1 }), count: w.reviews },
                    )}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </>
  );
}

interface Bar {
  key: string;
  label: string;
  online?: number;
  pos?: number;
  total?: number;
}

/** Bar columns (stacked online and counter when both are given), with the numbers as a hidden table. */
function Bars({ caption, bars }: { caption: string; bars: readonly Bar[] }) {
  const intl = useIntl();
  const stacked = bars.some((b) => b.online !== undefined);
  const sum = (b: Bar) => (b.online ?? 0) + (b.pos ?? 0) + (b.total ?? 0);
  const max = Math.max(1, ...bars.map(sum));
  // Labels under at most ~10 columns; the rest stay in the hidden table.
  const every = Math.max(1, Math.ceil(bars.length / 10));
  const pct = (v: number) => `${(v / max) * 100}%`;
  return (
    <>
      {stacked && (
        <div className={styles.legend} aria-hidden="true">
          <span>
            <i className={styles.swatchOnline} />
            {intl.formatMessage({ id: 'analytics.series.online' })}
          </span>
          <span>
            <i className={styles.swatchPos} />
            {intl.formatMessage({ id: 'analytics.series.pos' })}
          </span>
        </div>
      )}
      <div className={styles.bars} aria-hidden="true">
        {bars.map((b, i) => (
          <div key={b.key} className={styles.col} title={`${b.label}: ${formatMoney(sum(b), 'dashboard')}`}>
            <div className={styles.stackBar}>
              {stacked ? (
                <>
                  <i className={styles.segPos} style={{ height: pct(b.pos ?? 0) }} />
                  <i className={styles.segOnline} style={{ height: pct(b.online ?? 0) }} />
                </>
              ) : (
                <i className={styles.segPos} style={{ height: pct(b.total ?? 0) }} />
              )}
            </div>
            <span className={styles.colLabel}>{i % every === 0 ? b.label : ''}</span>
          </div>
        ))}
      </div>
      <VisuallyHidden>
        <table>
          <caption>{caption}</caption>
          <tbody>
            {bars.map((b) => (
              <tr key={b.key}>
                <th scope="row">{b.label}</th>
                {stacked ? (
                  <>
                    <td>{`${intl.formatMessage({ id: 'analytics.series.online' })} ${formatMoney(b.online ?? 0, 'dashboard')}`}</td>
                    <td>{`${intl.formatMessage({ id: 'analytics.series.pos' })} ${formatMoney(b.pos ?? 0, 'dashboard')}`}</td>
                  </>
                ) : (
                  <td>{formatMoney(b.total ?? 0, 'dashboard')}</td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </VisuallyHidden>
    </>
  );
}

/** Online orders: received → accepted → collected, the missed reasons, and the time to accept. */
function Funnel({ f }: { f: OrderFunnel }) {
  const intl = useIntl();
  const steps = [
    { key: 'received', n: f.received },
    { key: 'accepted', n: f.accepted },
    { key: 'completed', n: f.completed },
  ] as const;
  const missed = [
    { key: 'rejected', n: f.rejected },
    { key: 'notAccepted', n: f.notAccepted },
    { key: 'noShow', n: f.noShow },
    { key: 'cancelled', n: f.cancelled },
  ] as const;
  return (
    <section className="tz-card">
      <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.funnel.title' })}</h2>
      {f.received === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'analytics.funnel.empty' })}</p>
      ) : (
        <>
          <ol className={styles.funnel}>
            {steps.map((s) => (
              <li key={s.key}>
                <span>{intl.formatMessage({ id: `analytics.funnel.${s.key}` })}</span>
                <b className="tz-num">{intl.formatNumber(s.n)}</b>
                <i style={{ width: `${(s.n / Math.max(1, f.received)) * 100}%` }} aria-hidden="true" />
              </li>
            ))}
          </ol>
          <ul className={styles.list} aria-label={intl.formatMessage({ id: 'analytics.funnel.missed' })}>
            {missed.map((m) => (
              <li key={m.key}>
                <span>{intl.formatMessage({ id: `analytics.funnel.${m.key}` })}</span>
                <span className="tz-num">{intl.formatNumber(m.n)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
      <hr className="tz-sep" />
      <p className={styles.note}>
        {f.p50AcceptSeconds === null
          ? intl.formatMessage({ id: 'analytics.accept.none' })
          : intl.formatMessage({ id: 'analytics.accept.p50' }, { time: duration(intl, f.p50AcceptSeconds) })}
      </p>
      <p className={styles.note}>
        {f.nearbyMedianAcceptSeconds === null
          ? intl.formatMessage({ id: 'analytics.accept.nearbyNone' })
          : intl.formatMessage({ id: 'analytics.accept.nearby' }, { time: duration(intl, f.nearbyMedianAcceptSeconds) })}
      </p>
    </section>
  );
}

function TopItems({ items }: { items: readonly AnalyticsTopItem[] }) {
  const intl = useIntl();
  const columns = useMemo<ColumnDef<AnalyticsTopItem, unknown>[]>(
    () => [
      {
        id: 'item',
        header: intl.formatMessage({ id: 'overview.top.item' }),
        enableSorting: false,
        cell: ({ row }) => (
          <>
            {row.original.name}
            <span className="tz-sub">{row.original.size}</span>
          </>
        ),
      },
      { id: 'sold', header: intl.formatMessage({ id: 'overview.top.sold' }), enableSorting: false, meta: { numeric: true }, cell: ({ row }) => row.original.sold },
      {
        id: 'revenue',
        header: intl.formatMessage({ id: 'overview.top.revenue' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) => formatMoney(row.original.revenuePaise, 'dashboard'),
      },
    ],
    [intl],
  );
  return (
    <section className={`tz-card ${styles.wide}`}>
      <h2 className="tz-card-title">{intl.formatMessage({ id: 'analytics.top.title' })}</h2>
      <DataTable
        label={intl.formatMessage({ id: 'analytics.top.title' })}
        columns={columns}
        data={items}
        getRowId={(r) => r.storeItemId}
        emptyLabel={intl.formatMessage({ id: 'overview.top.empty' })}
      />
    </section>
  );
}

function CategoryMix({ mix }: { mix: readonly { category: string; paise: number }[] }) {
  const intl = useIntl();
  const id = useId();
  const total = mix.reduce((n, c) => n + c.paise, 0);
  return (
    <section className="tz-card" aria-labelledby={id}>
      <h2 id={id} className="tz-card-title">
        {intl.formatMessage({ id: 'analytics.mix.title' })}
      </h2>
      {total === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'analytics.daily.empty' })}</p>
      ) : (
        <ul className={styles.mix}>
          {mix.map((c) => (
            <li key={c.category}>
              <span>{c.category}</span>
              <span className="tz-num">
                {intl.formatMessage(
                  { id: 'analytics.mix.value' },
                  { amount: formatMoney(c.paise, 'dashboard'), share: intl.formatNumber(c.paise / total, { style: 'percent' }) },
                )}
              </span>
              <i style={{ width: `${(c.paise / total) * 100}%` }} aria-hidden="true" />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
