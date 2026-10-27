// Counter sales (D-268): the store's POS sales next to online orders. Every device's sales as the
// server holds them, filtered by IST dates, payment, status, customer link, offline and cashier, with
// the totals strip and the CSV for owners and managers. On a device with an outbox, the sales that
// have not reached the server yet sit on top ("Not synced yet").
import { formatMoney } from '@todayz/i18n';
import type { PosSaleSummary } from '@todayz/api-client/gen/model/posSaleSummary';
import { Banner, Button, Chip, DataTable, EmptyState, KpiStrip, Skeleton, StatusPill, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { Download, Inbox, IndianRupee, ReceiptText, RefreshCw, Undo2, WalletCards } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { usePosMaybe } from '../pos/context';
import { PosProvider } from '../pos/runtime';
import { usePhone } from '../shell/useNarrow';
import { useNow } from './queries';
import { LinkPill, LinkWhy, SaleDetail } from './SaleDetail';
import {
  LINKS,
  PAYMENTS,
  RANGES,
  STATUSES,
  canSeeSalesTotals,
  istToday,
  rangeDays,
  salesCsv,
  salesParams,
  shortId,
  useSalesList,
  useSalesSummary,
  type SalesSearch,
} from './salesQueries';
import type { CurrentStore } from './store';
import styles from './sales.module.css';

export interface CounterSalesProps {
  store: CurrentStore;
  search: SalesSearch;
  /** Replaces the Counter sales part of the URL; `view=sales` is kept by the caller. */
  onSearch: (next: SalesSearch) => void;
}

const money = (p: number) => formatMoney(p, 'product');

/** Mounts the device runtime (for the outbox and receipts) around the view; the view works without it. */
export function CounterSales(props: CounterSalesProps) {
  const body = <CounterSalesBody {...props} />;
  return (
    <PosProvider store={props.store} fallback={body}>
      {body}
    </PosProvider>
  );
}

function CounterSalesBody({ store, search, onSearch }: CounterSalesProps) {
  const intl = useIntl();
  const phone = usePhone();
  const toast = useToast();
  const totals = canSeeSalesTotals(store.role);
  // "Today" is the IST date, checked every minute, so the ranges move at IST midnight.
  const today = istToday(useNow());
  const params = useMemo(() => salesParams(search, today), [search, today]);
  const list = useSalesList(store, params);
  const summary = useSalesSummary(store, params, totals);
  const [exporting, setExporting] = useState(false);
  // An undefined value removes that key from the URL.
  const set = (patch: { [K in keyof SalesSearch]?: SalesSearch[K] | undefined }) => {
    onSearch(Object.fromEntries(Object.entries({ ...search, ...patch }).filter(([, v]) => v !== undefined)) as SalesSearch);
  };
  const filtered = Boolean(search.payment || search.status || search.link || search.offline || search.cashier);
  const sales = list.data?.pages.flatMap((p) => p.items) ?? [];

  const exportCsv = async () => {
    setExporting(true);
    try {
      const csv = await salesCsv(store, params);
      const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }));
      const a = document.createElement('a');
      const days = rangeDays(search, today);
      a.href = url;
      a.download = `counter-sales-${days.from ?? 'start'}-${days.to ?? 'now'}.csv`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch {
      toast.show({ message: intl.formatMessage({ id: 'orders.sales.exportFailed' }) });
    } finally {
      setExporting(false);
    }
  };

  const when = (iso: string) => intl.formatDate(iso, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' });
  const cashierName = (n: string | null) => n ?? intl.formatMessage({ id: 'orders.sales.formerStaff' });
  const open = (id: string) => set({ sale: id });

  const columns = useMemo<ColumnDef<PosSaleSummary, unknown>[]>(
    () => [
      {
        id: 'time',
        header: intl.formatMessage({ id: 'orders.sales.col.time' }),
        cell: ({ row }) => <time dateTime={row.original.soldAt}>{when(row.original.soldAt)}</time>,
      },
      {
        id: 'bill',
        header: intl.formatMessage({ id: 'orders.sales.col.bill' }),
        cell: ({ row }) => (
          <button type="button" className={styles.billLink} onClick={() => open(row.original.id)} aria-label={intl.formatMessage({ id: 'orders.sales.open' }, { id: shortId(row.original.id) })}>
            #{shortId(row.original.id)}
          </button>
        ),
      },
      {
        id: 'items',
        header: intl.formatMessage({ id: 'orders.sales.col.items' }),
        cell: ({ row }) => intl.formatMessage({ id: 'orders.sales.items' }, { lines: row.original.lineCount, units: row.original.unitCount }),
      },
      {
        id: 'total',
        header: intl.formatMessage({ id: 'orders.sales.col.total' }),
        meta: { numeric: true },
        cell: ({ row }) => <b className="tz-num">{money(row.original.totalPaise)}</b>,
      },
      {
        id: 'payment',
        header: intl.formatMessage({ id: 'orders.sales.col.payment' }),
        cell: ({ row }) => intl.formatMessage({ id: `pos.pay.${row.original.paymentMethod}` }),
      },
      {
        id: 'credits',
        header: intl.formatMessage({ id: 'orders.sales.col.credits' }),
        meta: { numeric: true },
        cell: ({ row }) => <span className="tz-num">{row.original.creditsUsed}</span>,
      },
      {
        id: 'link',
        header: intl.formatMessage({ id: 'orders.sales.col.link' }),
        cell: ({ row }) => (
          <>
            <LinkPill link={row.original.link} />
            <LinkWhy link={row.original.link} />
          </>
        ),
      },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'orders.sales.col.status' }),
        cell: ({ row }) => <SaleTags sale={row.original} />,
      },
      {
        id: 'cashier',
        header: intl.formatMessage({ id: 'orders.sales.col.cashier' }),
        cell: ({ row }) => cashierName(row.original.cashier.name),
      },
    ],
    // `when`, `open` and `cashierName` only close over intl and the search.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [intl, search],
  );

  return (
    <>
      <Outbox />

      <section className={styles.controls} aria-label={intl.formatMessage({ id: 'orders.sales.filters' })}>
        <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'orders.sales.range.label' })}>
          {RANGES.map((r) => (
            <Chip
              key={r}
              pressed={(search.range ?? 'today') === r}
              onPressedChange={() => set(r === 'custom' ? { range: r, ...rangeDays(search, today) } : { range: r === 'today' ? undefined : r, from: undefined, to: undefined })}
            >
              {intl.formatMessage({ id: `orders.sales.range.${r}` })}
            </Chip>
          ))}
        </div>
        {search.range === 'custom' && (
          <div className={styles.dates}>
            <TextField
              type="date"
              label={intl.formatMessage({ id: 'orders.sales.from' })}
              value={search.from ?? ''}
              max={search.to}
              onChange={(e) => set({ from: e.target.value || undefined })}
            />
            <TextField
              type="date"
              label={intl.formatMessage({ id: 'orders.sales.to' })}
              value={search.to ?? ''}
              min={search.from}
              onChange={(e) => set({ to: e.target.value || undefined })}
            />
          </div>
        )}
        <div className={styles.filters}>
          <select
            className="tz-input"
            aria-label={intl.formatMessage({ id: 'orders.sales.filter.payment' })}
            value={search.payment ?? ''}
            onChange={(e) => set({ payment: (e.target.value || undefined) as SalesSearch['payment'] })}
          >
            <option value="">{intl.formatMessage({ id: 'orders.sales.filter.paymentAll' })}</option>
            {PAYMENTS.map((p) => (
              <option key={p} value={p}>
                {intl.formatMessage({ id: `pos.pay.${p}` })}
              </option>
            ))}
          </select>
          <select
            className="tz-input"
            aria-label={intl.formatMessage({ id: 'orders.sales.filter.status' })}
            value={search.status ?? ''}
            onChange={(e) => set({ status: (e.target.value || undefined) as SalesSearch['status'] })}
          >
            <option value="">{intl.formatMessage({ id: 'orders.sales.filter.statusAll' })}</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {intl.formatMessage({ id: `orders.sales.status.${s}` })}
              </option>
            ))}
          </select>
          <select
            className="tz-input"
            aria-label={intl.formatMessage({ id: 'orders.sales.filter.link' })}
            value={search.link ?? ''}
            onChange={(e) => set({ link: (e.target.value || undefined) as SalesSearch['link'] })}
          >
            <option value="">{intl.formatMessage({ id: 'orders.sales.filter.linkAll' })}</option>
            {LINKS.map((l) => (
              <option key={l} value={l}>
                {intl.formatMessage({ id: `orders.sales.link.${l}` })}
              </option>
            ))}
          </select>
          {totals && (
            <select
              className="tz-input"
              aria-label={intl.formatMessage({ id: 'orders.sales.filter.cashier' })}
              value={search.cashier ?? ''}
              onChange={(e) => set({ cashier: e.target.value || undefined })}
            >
              <option value="">{intl.formatMessage({ id: 'orders.sales.filter.cashierAll' })}</option>
              {(summary.data?.byCashier ?? []).map((c) => (
                <option key={c.cashier.id} value={c.cashier.id}>
                  {cashierName(c.cashier.name)}
                </option>
              ))}
              {search.cashier && !summary.data?.byCashier.some((c) => c.cashier.id === search.cashier) && (
                <option value={search.cashier}>{intl.formatMessage({ id: 'orders.sales.formerStaff' })}</option>
              )}
            </select>
          )}
          <Chip pressed={search.offline === true} onPressedChange={(on) => set({ offline: on ? true : undefined })}>
            {intl.formatMessage({ id: 'orders.sales.filter.offline' })}
          </Chip>
          {filtered && (
            <Button size="sm" variant="ghost" onClick={() => set({ payment: undefined, status: undefined, link: undefined, offline: undefined, cashier: undefined })}>
              {intl.formatMessage({ id: 'orders.sales.filter.clear' })}
            </Button>
          )}
          {totals && (
            <Button size="sm" icon={Download} loading={exporting} className={styles.export} onClick={() => void exportCsv()}>
              {intl.formatMessage({ id: 'orders.sales.export' })}
            </Button>
          )}
        </div>
      </section>

      {totals && <Stats summary={summary} />}

      {list.isError ? (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'orders.error.title' })}
          description={intl.formatMessage({ id: 'orders.error.body' })}
          action={<Button loading={list.isFetching} onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'orders.error.retry' })}</Button>}
        />
      ) : !list.data ? (
        <div role="status" aria-label={intl.formatMessage({ id: 'orders.sales.loading' })}>
          <Skeleton height="12rem" shape="block" />
        </div>
      ) : sales.length === 0 ? (
        <EmptyState icon={ReceiptText} title={intl.formatMessage({ id: 'orders.sales.empty.title' })} description={intl.formatMessage({ id: 'orders.sales.empty.body' })} />
      ) : phone ? (
        <ul className={styles.cards} aria-label={intl.formatMessage({ id: 'orders.view.counter' })}>
          {sales.map((s) => (
            <li key={s.id}>
              <button type="button" className={styles.card} onClick={() => open(s.id)} aria-label={intl.formatMessage({ id: 'orders.sales.open' }, { id: shortId(s.id) })}>
                <span className={styles.cardTop}>
                  <b className="tz-num">{money(s.totalPaise)}</b>
                  <time className="tz-sub" dateTime={s.soldAt}>
                    {when(s.soldAt)}
                  </time>
                </span>
                <span className="tz-sub">
                  #{shortId(s.id)} · {intl.formatMessage({ id: 'orders.sales.items' }, { lines: s.lineCount, units: s.unitCount })} · {intl.formatMessage({ id: `pos.pay.${s.paymentMethod}` })} ·{' '}
                  {cashierName(s.cashier.name)}
                </span>
                <span className={styles.tags}>
                  <SaleTags sale={s} />
                  <LinkPill link={s.link} />
                </span>
                <LinkWhy link={s.link} />
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'orders.view.counter' })}
          columns={columns}
          data={sales}
          getRowId={(s) => s.id}
          emptyLabel={intl.formatMessage({ id: 'orders.sales.empty.title' })}
        />
      )}

      {list.hasNextPage && (
        <Button variant="soft" className={styles.more} loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {intl.formatMessage({ id: 'orders.sales.more' })}
        </Button>
      )}

      <SaleDetail store={store} saleId={search.sale} onClose={() => set({ sale: undefined })} />
    </>
  );
}

function SaleTags({ sale }: { sale: Pick<PosSaleSummary, 'status' | 'offline'> }) {
  const intl = useIntl();
  return (
    <span className={styles.tags}>
      {sale.status === 'reversed' ? (
        <StatusPill tone="cancelled">{intl.formatMessage({ id: 'pos.history.state.reversed' })}</StatusPill>
      ) : (
        <StatusPill tone="completed">{intl.formatMessage({ id: 'orders.sales.status.completed' })}</StatusPill>
      )}
      {sale.offline && <StatusPill>{intl.formatMessage({ id: 'pos.storeSales.offlineSale' })}</StatusPill>}
    </span>
  );
}

function Stats({ summary }: { summary: ReturnType<typeof useSalesSummary> }) {
  const intl = useIntl();
  const s = summary.data;
  const loading = !s;
  if (summary.isError) return <Banner tone="warning">{intl.formatMessage({ id: 'orders.sales.kpi.failed' })}</Banner>;
  return (
    <>
      <KpiStrip
        label={intl.formatMessage({ id: 'orders.sales.kpi.label' })}
        items={[
          {
            key: 'sales',
            icon: WalletCards,
            name: intl.formatMessage({ id: 'orders.sales.kpi.sales' }),
            value: s ? money(s.salesPaise) : '',
            note: s ? intl.formatMessage({ id: 'orders.sales.kpi.bills' }, { count: s.billCount }) : undefined,
            loading,
          },
          {
            key: 'average',
            icon: IndianRupee,
            name: intl.formatMessage({ id: 'orders.sales.kpi.average' }),
            value: s ? money(s.averageBillPaise) : '',
            loading,
          },
          {
            key: 'reversed',
            icon: Undo2,
            name: intl.formatMessage({ id: 'orders.sales.kpi.reversed' }),
            value: s ? money(s.reversedPaise) : '',
            note: s ? intl.formatMessage({ id: 'orders.sales.kpi.bills' }, { count: s.reversedCount }) : undefined,
            loading,
          },
          {
            key: 'credits',
            icon: ReceiptText,
            name: intl.formatMessage({ id: 'orders.sales.kpi.credits' }),
            value: s ? intl.formatNumber(s.creditsUsed) : '',
            note: s ? intl.formatMessage({ id: 'orders.sales.kpi.cashback' }, { count: s.cashbackCredits }) : undefined,
            loading,
          },
        ]}
      />
      {s && s.byPayment.length > 0 && (
        <ul className={styles.split} aria-label={intl.formatMessage({ id: 'orders.sales.byPayment' })}>
          {s.byPayment.map((p) => (
            <li key={p.method}>
              <span>{intl.formatMessage({ id: `pos.pay.${p.method}` })}</span> <b className="tz-num">{money(p.totalPaise)}</b>{' '}
              <span className={styles.muted}>{intl.formatMessage({ id: 'orders.sales.kpi.bills' }, { count: p.billCount })}</span>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

/** This device's sales the server has not taken yet: waiting, sending or refused. */
function Outbox() {
  const intl = useIntl();
  const pos = usePosMaybe();
  const [retrying, setRetrying] = useState<string | null>(null);
  if (!pos) return null;
  const waiting = pos.records.filter((r) => r.state !== 'synced');
  if (waiting.length === 0) return null;
  const retry = async (clientId: string) => {
    setRetrying(clientId);
    try {
      await pos.engine.retry(clientId);
    } finally {
      setRetrying(null);
    }
  };
  return (
    <section className={styles.outbox} aria-label={intl.formatMessage({ id: 'orders.sales.outbox.title' })}>
      <div className={styles.cardTop}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'orders.sales.outbox.title' })}</h2>
        <Button size="sm" icon={RefreshCw} disabled={!pos.online} loading={pos.online && pos.sync.status === 'syncing'} onClick={() => void pos.engine.flush({ force: true })}>
          {intl.formatMessage({ id: 'pos.sync.now' })}
        </Button>
      </div>
      <p className="tz-sub">{intl.formatMessage({ id: 'orders.sales.outbox.body' })}</p>
      <ul className={styles.outboxList}>
        {waiting.map((r) => (
          <li key={r.sale.clientId} className={styles.cardTop}>
            <span>
              <b className="tz-num">{money(r.sale.totalPaise)}</b>{' '}
              <span className="tz-sub">
                {intl.formatMessage({ id: `pos.pay.${r.sale.paymentMethod}` })} · {intl.formatTime(r.sale.soldAt, { hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' })}
              </span>
            </span>
            <span className={styles.tags}>
              <StatusPill tone={r.state === 'rejected' ? 'rejected' : 'pending'}>{intl.formatMessage({ id: `pos.history.state.${r.state}` })}</StatusPill>
              {r.state === 'rejected' && (
                <Button size="sm" loading={retrying === r.sale.clientId} onClick={() => void retry(r.sale.clientId)}>
                  {intl.formatMessage({ id: 'pos.sync.retry' })}
                </Button>
              )}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
