import type { AdminAuditEntry } from '@todayz/api-client/gen/model/adminAuditEntry';
import { Button, DataTable, Sheet, TextField, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useProblemText } from '../app/problems';
import { ChangeList } from './ChangeList';
import { useActionLabel, useActorLabel } from './labels';
import { useAuditLog } from './queries';
import { SUBJECT_TYPES, auditParams, validateAuditSearch, type AuditSearch } from './search';
import css from '../app/page.module.css';

/** A-19 Audit log: every admin action and sign-in, newest first, server-paged; a row opens its change. */
export function AuditLogPage({ search }: { search: AuditSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const actionLabel = useActionLabel();
  const actorLabel = useActorLabel();
  const problemText = useProblemText();
  const navigate = useNavigate();
  const params = useMemo(() => auditParams(search), [search]);
  const q = useAuditLog(params);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const [open, setOpen] = useState<AdminAuditEntry | null>(null);

  const columns = useMemo<ColumnDef<AdminAuditEntry, unknown>[]>(
    () => [
      { id: 'when', header: intl.formatMessage({ id: 'audit.col.when' }), accessorFn: (e) => when(e.createdAt, now ?? e.createdAt) },
      { id: 'actor', header: intl.formatMessage({ id: 'audit.col.actor' }), accessorFn: (e) => actorLabel(e) },
      {
        id: 'action',
        header: intl.formatMessage({ id: 'audit.col.action' }),
        cell: ({ row }) => (
          <Button size="sm" variant="ghost" onClick={() => setOpen(row.original)}>
            {actionLabel(row.original.action)}
          </Button>
        ),
      },
      {
        id: 'subject',
        header: intl.formatMessage({ id: 'audit.col.subject' }),
        accessorFn: (e) => `${e.subjectType} · ${e.subjectId}`,
      },
    ],
    [intl, when, now, actorLabel, actionLabel],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.audit' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'audit.sub' })}</p>
      </div>
      <Filters key={JSON.stringify(search)} search={search} onApply={(next) => void navigate({ to: '/audit', search: next })} />
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'audit.table' })}
          columns={columns}
          data={rows}
          getRowId={(e) => e.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'audit.none' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
      <Sheet
        open={open !== null}
        onClose={() => setOpen(null)}
        title={open ? actionLabel(open.action) : ''}
        description={
          open
            ? intl.formatMessage({ id: 'audit.panel.by' }, { actor: actorLabel(open), when: when(open.createdAt, now ?? open.createdAt) })
            : undefined
        }
        closeLabel={intl.formatMessage({ id: 'common.close' })}
      >
        {open && (
          <>
            <p className={css.muted}>
              {open.subjectType} · {open.subjectId}
            </p>
            <ChangeList before={open.before} after={open.after} />
          </>
        )}
      </Sheet>
    </div>
  );
}

function Filters({ search, onApply }: { search: AuditSearch; onApply: (s: AuditSearch) => void }) {
  const intl = useIntl();
  const [draft, setDraft] = useState<Record<keyof AuditSearch, string>>({
    actorId: search.actorId ?? '',
    action: search.action ?? '',
    subjectType: search.subjectType ?? '',
    subjectId: search.subjectId ?? '',
    from: search.from ?? '',
    to: search.to ?? '',
  });
  const set = (k: keyof AuditSearch) => (e: { target: { value: string } }) => setDraft((d) => ({ ...d, [k]: e.target.value }));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onApply(validateAuditSearch(draft));
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'audit.filters' })}>
      <TextField label={intl.formatMessage({ id: 'audit.filter.actor' })} value={draft.actorId} onChange={set('actorId')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'audit.filter.action' })} value={draft.action} onChange={set('action')} autoComplete="off" />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="audit-type">
          {intl.formatMessage({ id: 'audit.filter.type' })}
        </label>
        <select id="audit-type" className="tz-input" value={draft.subjectType} onChange={set('subjectType')}>
          <option value="">{intl.formatMessage({ id: 'audit.filter.anyType' })}</option>
          {SUBJECT_TYPES.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>
      <TextField label={intl.formatMessage({ id: 'audit.filter.subject' })} value={draft.subjectId} onChange={set('subjectId')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'audit.filter.from' })} type="date" value={draft.from} onChange={set('from')} />
      <TextField label={intl.formatMessage({ id: 'audit.filter.to' })} type="date" value={draft.to} onChange={set('to')} />
      <div className={css.actions}>
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'audit.filter.apply' })}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onApply({})}>
          {intl.formatMessage({ id: 'audit.filter.clear' })}
        </Button>
      </div>
    </form>
  );
}
