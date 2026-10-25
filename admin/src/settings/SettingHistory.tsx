import type { AdminSetting } from '@todayz/api-client/gen/model/adminSetting';
import { Button, Skeleton } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useSettingLabels } from './labels';
import { useSettingHistory } from './queries';
import audit from '../audit/audit.module.css';

/** The audit side panel for A-17: the chosen setting's changes from the audit log, newest first. */
export function SettingHistory({ setting }: { setting: AdminSetting | undefined }) {
  const intl = useIntl();
  return (
    <aside className={`tz-card ${audit.panel}`} aria-labelledby="setting-history">
      <h2 id="setting-history" className="tz-card-title">
        {setting ? intl.formatMessage({ id: 'settings.history.title' }, { key: setting.key }) : intl.formatMessage({ id: 'audit.panel.title' })}
      </h2>
      {setting ? <Entries key={setting.key} setting={setting} /> : <p className={audit.meta}>{intl.formatMessage({ id: 'settings.history.pick' })}</p>}
    </aside>
  );
}

function Entries({ setting }: { setting: AdminSetting }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useSettingLabels();
  const q = useSettingHistory(setting.key);
  const pages = q.data?.pages ?? [];
  const items = pages.flatMap((p) => p.items.map((e) => ({ e, now: p.serverNow })));
  const show = (v: unknown) => labels.value(setting.unit, v);
  return (
    <>
      {q.isPending && <Skeleton shape="block" height="120px" />}
      {q.isError && (
        <p role="alert" className={audit.meta}>
          {intl.formatMessage({ id: 'audit.panel.failed' })}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      )}
      {q.isSuccess && items.length === 0 && <p className={audit.meta}>{intl.formatMessage({ id: 'settings.history.none' })}</p>}
      {items.length > 0 && (
        <ol className={audit.list}>
          {items.map(({ e, now }) => (
            <li key={e.id} className={audit.entry}>
              <strong>{intl.formatMessage({ id: 'settings.history.change' }, { from: show(e.from), to: show(e.to) })}</strong>
              <span className={audit.meta}>
                {intl.formatMessage(
                  { id: 'audit.panel.by' },
                  { actor: e.actorName ?? intl.formatMessage({ id: 'audit.actor.admin' }), when: when(e.createdAt, now) },
                )}
              </span>
              {e.reason && <span>{e.reason}</span>}
            </li>
          ))}
        </ol>
      )}
      {q.hasNextPage && (
        <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {intl.formatMessage({ id: 'common.loadMore' })}
        </Button>
      )}
    </>
  );
}
