import type { AdminAuditSubjectType } from '@todayz/api-client/gen/model/adminAuditSubjectType';
import { Button, Skeleton } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { ChangeList } from './ChangeList';
import { useActionLabel, useActorLabel } from './labels';
import { useSubjectAudit } from './queries';
import css from './audit.module.css';

/**
 * The audit side panel every admin detail screen shows next to its changes: this subject's
 * history, newest first, with each row's before/after. The screen refreshes it after a change.
 * A screen showing two subjects at once names each panel with `title`.
 */
export function AuditPanel({ type, id, title }: { type: AdminAuditSubjectType; id: string; title?: string }) {
  const intl = useIntl();
  const when = useWhen();
  const action = useActionLabel();
  const actor = useActorLabel();
  const q = useSubjectAudit(type, id);
  const pages = q.data?.pages ?? [];
  const items = pages.flatMap((p) => p.items.map((e) => ({ e, now: p.serverNow })));
  return (
    <aside className={`tz-card ${css.panel}`} aria-labelledby={`audit-${id}`}>
      <h2 id={`audit-${id}`} className="tz-card-title">
        {title ?? intl.formatMessage({ id: 'audit.panel.title' })}
      </h2>
      {q.isPending && <Skeleton shape="block" height="120px" />}
      {q.isError && (
        <p role="alert" className={css.meta}>
          {intl.formatMessage({ id: 'audit.panel.failed' })}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      )}
      {q.isSuccess && items.length === 0 && <p className={css.meta}>{intl.formatMessage({ id: 'audit.panel.empty' })}</p>}
      {items.length > 0 && (
        <ol className={css.list}>
          {items.map(({ e, now }) => (
            <li key={e.id} className={css.entry}>
              <strong>{action(e.action)}</strong>
              <span className={css.meta}>
                {intl.formatMessage({ id: 'audit.panel.by' }, { actor: actor(e), when: when(e.createdAt, now) })}
              </span>
              <ChangeList before={e.before} after={e.after} />
            </li>
          ))}
        </ol>
      )}
      {q.hasNextPage && (
        <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {intl.formatMessage({ id: 'common.loadMore' })}
        </Button>
      )}
    </aside>
  );
}
