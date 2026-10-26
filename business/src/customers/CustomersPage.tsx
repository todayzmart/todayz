import type { CustomerSegment } from '@todayz/api-client/gen/model/customerSegment';
import type { StoreCustomer } from '@todayz/api-client/gen/model/storeCustomer';
import { formatMoney } from '@todayz/i18n';
import { Button, Chip, DataTable, EmptyState, ToastProvider, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Inbox, Users } from 'lucide-react';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { StoreGate } from '../overview/StoreGate';
import { useCustomers } from './queries';
import type { CustomersSearch } from './search';
import styles from './customers.module.css';

/** A row with a key for the table: a phone-only link has no customer id. */
type Row = StoreCustomer & { key: string };

const SEGMENTS: readonly CustomerSegment[] = ['all', 'top', 'slipping'];

/** B-09 Customers: who bought here lately, the top spenders and the regulars slipping away (D-99). */
export function CustomersPage({ search }: { search: CustomersSearch }) {
  return (
    <ToastProvider>
      <StoreGate next="/customers">{(store) => <Customers store={store} segment={search.segment ?? 'all'} />}</StoreGate>
    </ToastProvider>
  );
}

function Customers({ store, segment }: { store: CurrentStore; segment: CustomerSegment }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const q = useCustomers(store, segment);
  const first = q.data?.pages[0];
  const rows = useMemo<Row[]>(
    () => (q.data?.pages.flatMap((p) => p.items) ?? []).map((c, i) => ({ ...c, key: c.customerId ?? `phone:${i}` })),
    [q.data],
  );
  const pick = (s: CustomerSegment) => void navigate({ to: '/customers', search: s === 'all' ? {} : { segment: s }, replace: true });
  const columns = useMemo<ColumnDef<Row, unknown>[]>(
    () => [
      {
        id: 'customer',
        header: intl.formatMessage({ id: 'customers.col.customer' }),
        enableSorting: false,
        cell: ({ row }) => (
          <>
            {row.original.name ?? row.original.phoneMasked ?? intl.formatMessage({ id: 'customers.unnamed' })}
            {row.original.name && row.original.phoneMasked && <span className="tz-sub">{row.original.phoneMasked}</span>}
          </>
        ),
      },
      {
        id: 'visits',
        header: intl.formatMessage({ id: 'customers.col.visits' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) => intl.formatNumber(row.original.visits),
      },
      {
        id: 'spend',
        header: intl.formatMessage({ id: 'customers.col.spend' }),
        enableSorting: false,
        meta: { numeric: true },
        cell: ({ row }) => formatMoney(row.original.spendPaise, 'dashboard'),
      },
      {
        id: 'last',
        header: intl.formatMessage({ id: 'customers.col.last' }),
        enableSorting: false,
        cell: ({ row }) =>
          intl.formatDate(new Date(row.original.lastVisitAt), { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' }),
      },
    ],
    [intl],
  );

  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'customers.title' })}</h1>
      <p className={styles.sub}>{store.name}</p>
      <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'customers.segments' })}>
        {SEGMENTS.map((s) => (
          <Chip key={s} pressed={segment === s} onPressedChange={() => pick(s)}>
            {intl.formatMessage({ id: `customers.segment.${s}` })}
          </Chip>
        ))}
      </div>
      {q.isError ? (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'customers.error' })}
          action={<Button loading={q.isFetching} onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'overview.retry' })}</Button>}
        />
      ) : first && !first.enough ? (
        <EmptyState
          icon={Users}
          title={intl.formatMessage({ id: 'customers.notEnough.title' })}
          description={intl.formatMessage({ id: 'customers.notEnough.body' })}
        />
      ) : (
        <section className="tz-card">
          {first && (
            <p className={styles.note}>
              {segment === 'slipping'
                ? intl.formatMessage({ id: 'customers.about.slipping' }, { days: first.lapseDays, total: first.total })
                : intl.formatMessage({ id: 'customers.about.window' }, { days: first.windowDays, total: first.total })}
            </p>
          )}
          <DataTable
            label={intl.formatMessage({ id: 'customers.title' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.key}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: segment === 'slipping' ? 'customers.empty.slipping' : 'customers.empty' })}
          />
          {q.hasNextPage && (
            <div className={styles.more}>
              <Button loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'customers.more' })}
              </Button>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
