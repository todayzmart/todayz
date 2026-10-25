import type { AdminStoreStatus } from '@todayz/api-client/gen/model/adminStoreStatus';
import type { AdminStoreSummary } from '@todayz/api-client/gen/model/adminStoreSummary';
import { Button, DataTable, StatusPill, Tabs, TextField, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { STORE_STATUSES, storeTone, useStoreLabels } from './labels';
import { useStores } from './queries';
import type { StoresSearch } from './search';
import css from '../app/page.module.css';

type Go = (next: StoresSearch) => void;

/** A-05 Stores: every store with status and city filters, and the review queue (pending review). */
export function StoresPage({ search }: { search: StoresSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const go: Go = (next) => void navigate({ to: '/stores', search: next });
  const tab = search.tab ?? 'all';
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.stores' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'stores.sub' })}</p>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'stores.tabs' })}
        tabs={[
          { value: 'all', label: intl.formatMessage({ id: 'stores.tab.all' }) },
          { value: 'review', label: intl.formatMessage({ id: 'stores.tab.review' }) },
        ]}
        value={tab}
        onChange={(v) => go(v === 'review' ? { tab: 'review' } : {})}
      />
      {tab === 'all' && <Filters search={search} go={go} />}
      <List status={tab === 'review' ? 'pending_review' : search.status} city={tab === 'review' ? undefined : search.city} />
    </div>
  );
}

function Filters({ search, go }: { search: StoresSearch; go: Go }) {
  const intl = useIntl();
  const labels = useStoreLabels();
  const [city, setCity] = useState(search.city ?? '');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const c = city.trim();
    go({ ...(search.status ? { status: search.status } : {}), ...(c ? { city: c } : {}) });
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'stores.filters' })}>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="stores-status">
          {intl.formatMessage({ id: 'stores.col.status' })}
        </label>
        <select
          id="stores-status"
          className="tz-input"
          value={search.status ?? ''}
          onChange={(e) =>
            go({ ...(e.target.value ? { status: e.target.value as AdminStoreStatus } : {}), ...(search.city ? { city: search.city } : {}) })
          }
        >
          <option value="">{intl.formatMessage({ id: 'stores.status.all' })}</option>
          {STORE_STATUSES.map((s) => (
            <option key={s} value={s}>
              {labels.status(s)}
            </option>
          ))}
        </select>
      </div>
      <TextField label={intl.formatMessage({ id: 'stores.col.city' })} value={city} maxLength={60} onChange={(e) => setCity(e.target.value)} />
      <Button type="submit" variant="soft">
        {intl.formatMessage({ id: 'stores.search' })}
      </Button>
    </form>
  );
}

function List({ status, city }: { status?: AdminStoreStatus | undefined; city?: string | undefined }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useStoreLabels();
  const problemText = useProblemText();
  const filter = useMemo(() => ({ ...(status ? { status } : {}), ...(city ? { city } : {}) }), [status, city]);
  const q = useStores(filter);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminStoreSummary, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'stores.col.name' }),
        cell: ({ row }) => (
          <Link to="/stores/$storeId" params={{ storeId: row.original.id }}>
            {row.original.name}
          </Link>
        ),
      },
      {
        id: 'business',
        header: intl.formatMessage({ id: 'stores.col.business' }),
        cell: ({ row }) => (
          <Link to="/businesses/$businessId" params={{ businessId: row.original.businessId }}>
            {row.original.businessName}
          </Link>
        ),
      },
      {
        id: 'place',
        header: intl.formatMessage({ id: 'stores.col.place' }),
        accessorFn: (s) => [s.locality, s.city].filter(Boolean).join(', ') || '—',
      },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'stores.col.status' }),
        cell: ({ row }) => (
          <span className={css.title}>
            <StatusPill tone={storeTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>
            {row.original.closedUntil && now && Date.parse(row.original.closedUntil) > Date.parse(now) && (
              <StatusPill tone="cancelled">{intl.formatMessage({ id: 'stores.closed' })}</StatusPill>
            )}
          </span>
        ),
      },
      { id: 'strikes', header: intl.formatMessage({ id: 'stores.col.strikes' }), accessorFn: (s) => s.activeStrikes, meta: { numeric: true } },
      {
        id: 'reliability',
        header: intl.formatMessage({ id: 'stores.col.reliability' }),
        accessorFn: (s) => labels.percent(s.reliabilityScore),
        meta: { numeric: true },
      },
      { id: 'created', header: intl.formatMessage({ id: 'stores.col.created' }), accessorFn: (s) => when(s.createdAt, now ?? s.createdAt) },
    ],
    [intl, labels, when, now],
  );

  if (q.isError)
    return (
      <p role="alert" className="tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  return (
    <DataTable
      label={intl.formatMessage({ id: 'stores.table' })}
      columns={columns}
      data={rows}
      getRowId={(s) => s.id}
      loading={q.isPending}
      emptyLabel={intl.formatMessage({ id: status === 'pending_review' && !city ? 'stores.review.empty' : 'stores.empty' })}
      footer={
        q.hasNextPage ? (
          <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            {intl.formatMessage({ id: 'common.loadMore' })}
          </Button>
        ) : undefined
      }
    />
  );
}
