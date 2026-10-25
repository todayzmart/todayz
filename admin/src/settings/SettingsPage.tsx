import type { AdminProviderKey } from '@todayz/api-client/gen/model/adminProviderKey';
import type { AdminSetting } from '@todayz/api-client/gen/model/adminSetting';
import type { AdminSettingGroup } from '@todayz/api-client/gen/model/adminSettingGroup';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, Skeleton, StatusPill, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { SlidersHorizontal } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { StepUpDialog } from '../app/StepUpDialog';
import { SettingDialog } from './SettingDialog';
import { SettingHistory } from './SettingHistory';
import { useSettingLabels } from './labels';
import { useSettings, useUpdateSetting, type SettingChange } from './queries';
import { GROUPS, type SettingsSearch } from './search';
import css from '../app/page.module.css';
import styles from './settings.module.css';

/**
 * A-17 Platform settings: every 01 §17 value by group with its unit, range and last change; a
 * change needs a reason and a fresh authenticator code, and money settings are super admin only.
 * Provider keys show set or missing, never a value (D-19).
 */
export function SettingsPage({ search }: { search: SettingsSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const labels = useSettingLabels();
  const navigate = useNavigate();
  const q = useSettings();
  const update = useUpdateSetting();
  const [editing, setEditing] = useState<AdminSetting | null>(null);
  /** The change waiting on a step-up code; sent again once the code is right. */
  const [pending, setPending] = useState<SettingChange | null>(null);
  const now = q.data?.serverNow;

  const go = useCallback((next: SettingsSearch) => void navigate({ to: '/settings', search: next }), [navigate]);

  const save = (change: SettingChange) => {
    update.mutate(change, {
      onSuccess: (s) => {
        setEditing(null);
        setPending(null);
        update.reset();
        toast.show({ message: intl.formatMessage({ id: 'settings.done' }, { key: s.key }) });
        go({ ...search, key: s.key });
      },
      onError: (e) => {
        if (e instanceof ApiError && e.code === 'step_up_required') setPending(change);
      },
    });
  };

  const columns = useMemo<ColumnDef<AdminSetting, unknown>[]>(
    () => [
      {
        id: 'key',
        header: intl.formatMessage({ id: 'settings.col.setting' }),
        cell: ({ row }) => (
          <div>
            <Button size="sm" variant="ghost" aria-pressed={search.key === row.original.key} onClick={() => go({ ...search, key: row.original.key })}>
              <span className={css.mono}>{row.original.key}</span>
            </Button>
            <div className={css.muted}>{row.original.description}</div>
          </div>
        ),
      },
      {
        id: 'value',
        header: intl.formatMessage({ id: 'settings.col.value' }),
        accessorFn: (s) => labels.value(s.unit, s.value),
        meta: { numeric: true },
      },
      { id: 'range', header: intl.formatMessage({ id: 'settings.col.range' }), accessorFn: (s) => labels.range(s.unit, s.min, s.max) },
      {
        id: 'updated',
        header: intl.formatMessage({ id: 'settings.col.updated' }),
        accessorFn: (s) =>
          s.updatedByName
            ? intl.formatMessage({ id: 'settings.updatedBy' }, { when: when(s.updatedAt, now ?? s.updatedAt), name: s.updatedByName })
            : when(s.updatedAt, now ?? s.updatedAt),
      },
      {
        id: 'edit',
        header: intl.formatMessage({ id: 'settings.col.actions' }),
        cell: ({ row }) =>
          row.original.editable ? (
            <Button size="sm" onClick={() => setEditing(row.original)} aria-label={intl.formatMessage({ id: 'settings.change.named' }, { key: row.original.key })}>
              {intl.formatMessage({ id: 'settings.change' })}
            </Button>
          ) : (
            <span className={css.muted}>{intl.formatMessage({ id: row.original.money ? 'settings.superOnly' : 'settings.readOnly' })}</span>
          ),
      },
    ],
    [intl, labels, when, now, search, go],
  );

  if (q.isPending) return <Skeleton shape="block" height="320px" />;
  if (q.isError) {
    return (
      <EmptyState
        icon={SlidersHorizontal}
        headingLevel={2}
        title={intl.formatMessage({ id: 'settings.loadFailed' })}
        action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>}
      />
    );
  }

  const shown = GROUPS.filter((g) => !search.group || g === search.group)
    .map((g) => ({ g, items: q.data.items.filter((s) => s.group === g) }))
    .filter(({ items }) => items.length > 0);
  const selected = search.key ? q.data.items.find((s) => s.key === search.key) : undefined;

  return (
    <div className={css.column}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.settings' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'settings.sub' })}</p>
        </div>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="settings-group">
            {intl.formatMessage({ id: 'settings.filter.group' })}
          </label>
          <select
            id="settings-group"
            className="tz-input"
            value={search.group ?? ''}
            onChange={(e) => {
              const v = e.target.value;
              go(v ? { ...search, group: v as AdminSettingGroup } : search.key ? { key: search.key } : {});
            }}
          >
            <option value="">{intl.formatMessage({ id: 'settings.filter.all' })}</option>
            {GROUPS.filter((g) => q.data.items.some((s) => s.group === g)).map((g) => (
              <option key={g} value={g}>
                {labels.group(g)}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className={css.detail}>
        <div className={css.column}>
          {shown.map(({ g, items }) => (
            <section key={g} className="tz-card" aria-labelledby={`group-${g}`}>
              <h2 id={`group-${g}`} className="tz-card-title">
                {labels.group(g)}
              </h2>
              <DataTable label={labels.group(g)} columns={columns} data={items} getRowId={(s) => s.key} emptyLabel={intl.formatMessage({ id: 'settings.none' })} />
            </section>
          ))}
          <Providers items={q.data.providers} />
        </div>
        <SettingHistory setting={selected} />
      </div>

      {editing && (
        <SettingDialog
          // A fresh form per setting.
          key={editing.key}
          setting={editing}
          pending={update.isPending}
          error={update.error}
          onSave={save}
          onClose={() => {
            setEditing(null);
            setPending(null);
            update.reset();
          }}
        />
      )}
      <StepUpDialog
        open={pending !== null}
        onVerified={() => {
          if (pending) save(pending);
        }}
        onClose={() => {
          setPending(null);
          update.reset();
        }}
      />
    </div>
  );
}

/** Provider keys come from the environment (D-19): the name and whether it is set, never the value. */
function Providers({ items }: { items: AdminProviderKey[] }) {
  const intl = useIntl();
  if (items.length === 0) return null;
  return (
    <section className="tz-card" aria-labelledby="providers-title">
      <h2 id="providers-title" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.providers' })}
      </h2>
      <p className={css.muted}>{intl.formatMessage({ id: 'settings.providers.sub' })}</p>
      <ul className={styles.providers}>
        {items.map((p) => (
          <li key={p.name} className={styles.provider}>
            <span className={css.mono}>{p.name}</span>
            <StatusPill tone={p.set ? 'completed' : 'pending'}>
              {intl.formatMessage({ id: p.set ? 'settings.providers.set' : 'settings.providers.missing' })}
            </StatusPill>
          </li>
        ))}
      </ul>
    </section>
  );
}
