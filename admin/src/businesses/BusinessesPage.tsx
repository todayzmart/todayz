import type { AdminBusinessSummary } from '@todayz/api-client/gen/model/adminBusinessSummary';
import type { AdminLead } from '@todayz/api-client/gen/model/adminLead';
import type { AdminLeadStatus } from '@todayz/api-client/gen/model/adminLeadStatus';
import { Button, DataTable, StatusPill, Tabs, useToast, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { leadTone, statusTone, useBusinessLabels } from './labels';
import { useBusinesses, useLeadStatus, useLeads } from './queries';
import { LEAD_STATUSES, QUEUES, type BusinessesSearch, type Queue } from './search';
import css from '../app/page.module.css';

type Go = (next: BusinessesSearch) => void;

/**
 * A-03 Businesses: the verification queue by status (support reads, admins act on the detail) and
 * the Leads tab. A field agent sees only the Leads tab; the API refuses them the queue.
 */
export function BusinessesPage({ search }: { search: BusinessesSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const roles = useRoles();
  const labels = useBusinessLabels();
  const go: Go = (next) => void navigate({ to: '/businesses', search: next });
  const mayQueue = can(roles, 'businessView');
  const tab = !mayQueue || search.tab === 'leads' ? 'leads' : (search.status ?? 'submitted');
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.businesses' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: mayQueue ? 'businesses.sub' : 'businesses.sub.leads' })}</p>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'businesses.tabs' })}
        tabs={[
          ...(mayQueue ? QUEUES.map((s) => ({ value: s, label: labels.status(s) })) : []),
          { value: 'leads', label: intl.formatMessage({ id: 'businesses.tab.leads' }) },
        ]}
        value={tab}
        onChange={(v) => go(v === 'leads' ? { tab: 'leads' } : v === 'submitted' ? {} : { status: v as Queue })}
      />
      {tab === 'leads' ? <LeadsTab search={search} go={go} /> : <QueueList status={tab} />}
    </div>
  );
}

function QueueList({ status }: { status: Queue }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useBusinessLabels();
  const problemText = useProblemText();
  const q = useBusinesses(status);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminBusinessSummary, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'businesses.col.name' }),
        cell: ({ row }) => (
          <Link to="/businesses/$businessId" params={{ businessId: row.original.id }}>
            {row.original.brandName}
          </Link>
        ),
      },
      { id: 'legal', header: intl.formatMessage({ id: 'businesses.col.legal' }), accessorFn: (b) => b.legalName ?? '—' },
      { id: 'city', header: intl.formatMessage({ id: 'businesses.col.city' }), accessorFn: (b) => b.city ?? '—' },
      { id: 'stores', header: intl.formatMessage({ id: 'businesses.col.stores' }), accessorFn: (b) => b.storeCount, meta: { numeric: true } },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'businesses.col.status' }),
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      {
        id: 'submitted',
        header: intl.formatMessage({ id: 'businesses.col.submitted' }),
        accessorFn: (b) => (b.submittedAt ? when(b.submittedAt, now ?? b.submittedAt) : '—'),
      },
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
      label={intl.formatMessage({ id: 'businesses.table' }, { status: labels.status(status) })}
      columns={columns}
      data={rows}
      getRowId={(b) => b.id}
      loading={q.isPending}
      emptyLabel={intl.formatMessage({ id: 'businesses.empty' })}
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

/** Site leads with a status control; the phone is masked for every role. */
function LeadsTab({ search, go }: { search: BusinessesSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useBusinessLabels();
  const problemText = useProblemText();
  const q = useLeads(search.lead);
  const move = useLeadStatus();
  const [moving, setMoving] = useState<{ lead: AdminLead; to: AdminLeadStatus } | null>(null);
  const may = can(roles, 'leads');
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminLead, unknown>[]>(
    () => [
      { id: 'shop', header: intl.formatMessage({ id: 'businesses.leads.col.shop' }), accessorFn: (l) => l.shopName },
      { id: 'name', header: intl.formatMessage({ id: 'businesses.leads.col.name' }), accessorFn: (l) => l.name },
      { id: 'phone', header: intl.formatMessage({ id: 'businesses.leads.col.phone' }), accessorFn: (l) => l.phoneMasked ?? '—' },
      { id: 'city', header: intl.formatMessage({ id: 'businesses.col.city' }), accessorFn: (l) => l.city },
      { id: 'source', header: intl.formatMessage({ id: 'businesses.leads.col.source' }), accessorFn: (l) => l.source },
      { id: 'at', header: intl.formatMessage({ id: 'businesses.leads.col.at' }), accessorFn: (l) => when(l.createdAt, now ?? l.createdAt) },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'businesses.col.status' }),
        cell: ({ row }) =>
          may ? (
            <select
              className="tz-input"
              aria-label={intl.formatMessage({ id: 'businesses.leads.move' }, { shop: row.original.shopName })}
              value={row.original.status}
              onChange={(e) => {
                move.reset();
                setMoving({ lead: row.original, to: e.target.value as AdminLeadStatus });
              }}
            >
              {LEAD_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {labels.lead(s)}
                </option>
              ))}
            </select>
          ) : (
            <StatusPill tone={leadTone(row.original.status)}>{labels.lead(row.original.status)}</StatusPill>
          ),
      },
    ],
    [intl, labels, when, now, may, move],
  );

  return (
    <div className={css.column}>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="leads-status">
          {intl.formatMessage({ id: 'businesses.leads.filter' })}
        </label>
        <select
          id="leads-status"
          className="tz-input"
          value={search.lead ?? ''}
          onChange={(e) => go({ tab: 'leads', ...(e.target.value ? { lead: e.target.value as AdminLeadStatus } : {}) })}
        >
          <option value="">{intl.formatMessage({ id: 'businesses.leads.all' })}</option>
          {LEAD_STATUSES.map((s) => (
            <option key={s} value={s}>
              {labels.lead(s)}
            </option>
          ))}
        </select>
      </div>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'businesses.leads.table' })}
          columns={columns}
          data={rows}
          getRowId={(l) => l.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'businesses.leads.empty' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
      <ReasonDialog
        open={moving !== null}
        title={moving ? intl.formatMessage({ id: 'businesses.leads.moveTitle' }, { shop: moving.lead.shopName, status: labels.lead(moving.to) }) : ''}
        description={intl.formatMessage({ id: 'businesses.leads.moveBody' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.leads.moveConfirm' })}
        label={intl.formatMessage({ id: 'businesses.note.label' })}
        help={intl.formatMessage({ id: 'businesses.note.optional' })}
        optional
        pending={move.isPending}
        error={move.isError ? problemText(move.error) : null}
        onConfirm={(note) => {
          if (!moving) return;
          move.mutate(
            { id: moving.lead.id, status: moving.to, note },
            {
              onSuccess: () => {
                setMoving(null);
                toast.show({ message: intl.formatMessage({ id: 'businesses.leads.moved' }) });
              },
            },
          );
        }}
        onClose={() => setMoving(null)}
      />
    </div>
  );
}
