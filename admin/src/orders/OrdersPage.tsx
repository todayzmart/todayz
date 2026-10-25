import type { AdminOrderSummary } from '@todayz/api-client/gen/model/adminOrderSummary';
import { Button, DataTable, StatusPill, TextField, orderStatusTone, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { STATUSES, useOrderLabels } from './labels';
import { useOrders } from './queries';
import { ordersParams, validateOrdersSearch, type OrdersSearch } from './search';
import css from '../app/page.module.css';

/** A-07 Orders: server-paged search by number, store, customer, status and placed date (IST). */
export function OrdersPage({ search }: { search: OrdersSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useOrderLabels();
  const problemText = useProblemText();
  const navigate = useNavigate();
  const params = useMemo(() => ordersParams(search), [search]);
  const q = useOrders(params);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminOrderSummary, unknown>[]>(
    () => [
      {
        id: 'number',
        header: intl.formatMessage({ id: 'orders.col.number' }),
        cell: ({ row }) => (
          <Link to="/orders/$orderId" params={{ orderId: row.original.id }}>
            {row.original.number}
          </Link>
        ),
      },
      { id: 'store', header: intl.formatMessage({ id: 'orders.col.store' }), accessorFn: (o) => o.storeName },
      {
        id: 'customer',
        header: intl.formatMessage({ id: 'orders.col.customer' }),
        cell: ({ row }) => (
          <Link to="/customers/$customerId" params={{ customerId: row.original.customerId }}>
            {row.original.customerName ?? intl.formatMessage({ id: 'orders.noName' })}
          </Link>
        ),
      },
      { id: 'fulfilment', header: intl.formatMessage({ id: 'orders.col.fulfilment' }), accessorFn: (o) => labels.fulfilment(o.fulfilment) },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'orders.col.status' }),
        cell: ({ row }) => <StatusPill tone={orderStatusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'total', header: intl.formatMessage({ id: 'orders.col.total' }), accessorFn: (o) => money(o.totalPaise), meta: { numeric: true } },
      { id: 'placed', header: intl.formatMessage({ id: 'orders.col.placed' }), accessorFn: (o) => when(o.placedAt, now ?? o.placedAt) },
    ],
    [intl, labels, when, now],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.orders' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'orders.sub' })}</p>
      </div>
      <Filters
        key={JSON.stringify(search)}
        search={search}
        onApply={(next) => void navigate({ to: '/orders', search: next })}
      />
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'orders.table' })}
          columns={columns}
          data={rows}
          getRowId={(o) => o.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'orders.none' })}
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
  );
}

function Filters({ search, onApply }: { search: OrdersSearch; onApply: (s: OrdersSearch) => void }) {
  const intl = useIntl();
  const labels = useOrderLabels();
  const [draft, setDraft] = useState<Record<keyof OrdersSearch, string>>({
    number: search.number ?? '',
    status: search.status ?? '',
    storeId: search.storeId ?? '',
    customerId: search.customerId ?? '',
    from: search.from ?? '',
    to: search.to ?? '',
  });
  const set = (k: keyof OrdersSearch) => (e: { target: { value: string } }) => setDraft((d) => ({ ...d, [k]: e.target.value }));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onApply(validateOrdersSearch(draft));
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'orders.filters' })}>
      <TextField label={intl.formatMessage({ id: 'orders.filter.number' })} value={draft.number} onChange={set('number')} autoComplete="off" />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="orders-status">
          {intl.formatMessage({ id: 'orders.filter.status' })}
        </label>
        <select id="orders-status" className="tz-input" value={draft.status} onChange={set('status')}>
          <option value="">{intl.formatMessage({ id: 'orders.filter.anyStatus' })}</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {labels.status(s)}
            </option>
          ))}
        </select>
      </div>
      <TextField label={intl.formatMessage({ id: 'orders.filter.store' })} value={draft.storeId} onChange={set('storeId')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'orders.filter.customer' })} value={draft.customerId} onChange={set('customerId')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'orders.filter.from' })} type="date" value={draft.from} onChange={set('from')} />
      <TextField label={intl.formatMessage({ id: 'orders.filter.to' })} type="date" value={draft.to} onChange={set('to')} />
      <div className={css.actions}>
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'orders.filter.apply' })}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onApply({})}>
          {intl.formatMessage({ id: 'orders.filter.clear' })}
        </Button>
      </div>
    </form>
  );
}
