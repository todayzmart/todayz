import type { TicketStatus } from '@todayz/api-client/gen/model/ticketStatus';
import type { TicketSummary } from '@todayz/api-client/gen/model/ticketSummary';
import { Button, DataTable, Segmented, StatusPill, Tabs, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { useSupportLabels } from './labels';
import { useTickets } from './queries';
import { OPENERS, STATUSES, ticketsParams, validateSupportSearch, type SupportSearch } from './search';
import css from '../app/page.module.css';

type Whose = 'all' | 'me' | 'unassigned';

/** The search with one filter set or, for an empty value, removed. */
function patch(s: SupportSearch, key: keyof SupportSearch, value: string | undefined): SupportSearch {
  const next: Record<string, unknown> = { ...s, [key]: value };
  return validateSupportSearch(next);
}

/**
 * A-14 Support inbox: one tab per status with its count, open tickets soonest-due first with the
 * SLA chip (from serverNow), filters for whose tickets and who opened them, all kept in the URL.
 */
export function InboxPage({ search }: { search: SupportSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useSupportLabels();
  const problemText = useProblemText();
  const navigate = useNavigate();
  const go = (next: SupportSearch) => void navigate({ to: '/support', search: next });
  const params = useMemo(() => ticketsParams(search), [search]);
  const q = useTickets(params);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const first = pages[0];
  const now = first?.serverNow;
  const status = params.status ?? 'open';

  const columns = useMemo<ColumnDef<TicketSummary, unknown>[]>(
    () => [
      {
        id: 'ticket',
        header: intl.formatMessage({ id: 'support.col.ticket' }),
        cell: ({ row }) => (
          <Link to="/support/$ticketId" params={{ ticketId: row.original.id }}>
            {row.original.number} · {row.original.subject}
          </Link>
        ),
      },
      {
        id: 'from',
        header: intl.formatMessage({ id: 'support.col.from' }),
        accessorFn: (t) =>
          t.openerType === 'business'
            ? intl.formatMessage({ id: 'support.from.business' }, { name: labels.name(t.opener.name), business: t.businessName ?? '' })
            : intl.formatMessage({ id: 'support.from.customer' }, { name: labels.name(t.opener.name) }),
      },
      { id: 'category', header: intl.formatMessage({ id: 'support.col.category' }), accessorFn: (t) => labels.category(t.category) },
      {
        id: 'priority',
        header: intl.formatMessage({ id: 'support.col.priority' }),
        cell: ({ row }) => <StatusPill tone={labels.priorityTone(row.original.priority)}>{labels.priority(row.original.priority)}</StatusPill>,
      },
      {
        id: 'sla',
        header: intl.formatMessage({ id: 'support.col.sla' }),
        cell: ({ row }) => {
          const sla = now ? labels.sla(row.original, now) : null;
          return sla ? <StatusPill tone={sla.tone}>{sla.text}</StatusPill> : labels.status(row.original.status);
        },
      },
      {
        id: 'assignee',
        header: intl.formatMessage({ id: 'support.col.assignee' }),
        accessorFn: (t) => (t.assignee ? labels.name(t.assignee.name) : intl.formatMessage({ id: 'support.unassigned' })),
      },
      { id: 'last', header: intl.formatMessage({ id: 'support.col.last' }), accessorFn: (t) => when(t.lastMessageAt, now ?? t.lastMessageAt) },
    ],
    [intl, labels, when, now],
  );

  const whose: Whose = search.assignee ?? 'all';
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.support' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'support.sub' })}</p>
      </div>
      <Tabs<TicketStatus>
        label={intl.formatMessage({ id: 'support.tabs' })}
        tabs={STATUSES.map((s) => ({ value: s, label: labels.status(s), count: first?.counts[s] }))}
        value={status}
        onChange={(s) => go(patch(search, 'status', s))}
      />
      <div className={css.filters} role="group" aria-label={intl.formatMessage({ id: 'support.filters' })}>
        <Segmented<Whose>
          label={intl.formatMessage({ id: 'support.filter.whose' })}
          options={[
            { value: 'all', label: intl.formatMessage({ id: 'support.filter.all' }) },
            { value: 'me', label: intl.formatMessage({ id: 'support.filter.me' }) },
            { value: 'unassigned', label: intl.formatMessage({ id: 'support.filter.unassigned' }) },
          ]}
          value={whose}
          onChange={(v) => go(patch(search, 'assignee', v === 'all' ? undefined : v))}
        />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="support-opener">
            {intl.formatMessage({ id: 'support.filter.opener' })}
          </label>
          <select
            id="support-opener"
            className="tz-input"
            value={search.opener ?? ''}
            onChange={(e) => go(patch(search, 'opener', e.target.value))}
          >
            <option value="">{intl.formatMessage({ id: 'support.filter.anyOpener' })}</option>
            {OPENERS.map((o) => (
              <option key={o} value={o}>
                {labels.opener(o)}
              </option>
            ))}
          </select>
        </div>
      </div>
      {first && first.counts.overdue > 0 && (
        <p className={css.muted} role="status">
          {intl.formatMessage({ id: 'support.overdueCount' }, { count: first.counts.overdue })}
        </p>
      )}
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'support.table' })}
          columns={columns}
          data={rows}
          getRowId={(t) => t.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'support.none' })}
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
