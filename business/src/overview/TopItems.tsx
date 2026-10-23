import { formatClock, formatMoney } from '@todayz/i18n';
import { DataTable, VisuallyHidden, type ColumnDef } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { RouterLink } from './RouterLink';
import styles from './overview.module.css';
import type { OverviewData, StockSource, TopItem } from './types';

function sourceText(intl: IntlShape, source: StockSource, at: string): string {
  const time = formatClock(at);
  return source === 'pos'
    ? intl.formatMessage({ id: 'overview.top.source.pos' }, { time })
    : source === 'online'
      ? intl.formatMessage({ id: 'overview.top.source.online' }, { time })
      : intl.formatMessage({ id: 'overview.top.source.voice' }, { time });
}

function Spark({ values, label }: { values: readonly number[]; label: string }) {
  const max = Math.max(1, ...values);
  return (
    <span className="tz-spark" role="img" aria-label={label}>
      {values.map((v, i) => (
        <i key={i} style={{ height: `${Math.round((v / max) * 100)}%` }} />
      ))}
    </span>
  );
}

/** Top items table: what sold, its last 7 days, revenue, stock and the source of the last stock change. */
export function TopItems({ data }: { data: OverviewData }) {
  const intl = useIntl();
  const columns = useMemo<ColumnDef<TopItem, unknown>[]>(
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
      {
        id: 'sold',
        header: intl.formatMessage({ id: 'overview.top.sold' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) => row.original.sold,
      },
      {
        id: 'last7',
        header: intl.formatMessage({ id: 'overview.top.last7' }),
        enableSorting: false,
        cell: ({ row }) => (
          <Spark
            values={row.original.last7}
            label={intl.formatMessage({ id: 'overview.top.spark' }, { values: row.original.last7.join(', ') })}
          />
        ),
      },
      {
        id: 'revenue',
        header: intl.formatMessage({ id: 'overview.top.revenue' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) => formatMoney(row.original.revenuePaise, 'dashboard'),
      },
      {
        id: 'stock',
        header: intl.formatMessage({ id: 'overview.top.stock' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) =>
          row.original.low ? (
            <span className="tz-stock tz-stock-low">
              <span aria-hidden="true">{row.original.inStock}</span>
              <VisuallyHidden>{intl.formatMessage({ id: 'overview.top.low' }, { count: row.original.inStock })}</VisuallyHidden>
            </span>
          ) : (
            row.original.inStock
          ),
      },
      {
        id: 'updated',
        header: intl.formatMessage({ id: 'overview.top.updated' }),
        enableSorting: false,
        cell: ({ row }) => sourceText(intl, row.original.lastChange.source, row.original.lastChange.at),
      },
    ],
    [intl],
  );
  const items = data.topItems;
  const sold = items.reduce((s, i) => s + i.sold, 0);
  const revenue = items.reduce((s, i) => s + i.revenuePaise, 0);
  return (
    <section className={`tz-card ${styles.top}`} aria-labelledby="ov-top-title">
      <div className={`tz-card-head ${styles.topHead}`}>
        <h2 className="tz-card-title" id="ov-top-title">
          {data.period === 'today'
            ? intl.formatMessage({ id: 'overview.top.title' })
            : intl.formatMessage({ id: 'overview.top.titlePeriod' })}
        </h2>
        <RouterLink href="/stock" className={styles.headLink}>
          {intl.formatMessage({ id: 'overview.top.viewAll' })}
        </RouterLink>
      </div>
      <DataTable<TopItem>
        label={intl.formatMessage({ id: 'overview.top.label' })}
        columns={columns}
        data={items}
        getRowId={(r) => r.id}
        emptyLabel={intl.formatMessage({ id: 'overview.top.empty' })}
        footer={
          <>
            <span>{intl.formatMessage({ id: 'overview.top.total' }, { shown: items.length, all: data.distinctItemsSold })}</span>
            <span className={styles.totals}>
              {intl.formatMessage({ id: 'overview.top.totals' }, { sold, revenue: formatMoney(revenue, 'dashboard') })}
            </span>
          </>
        }
      />
    </section>
  );
}
