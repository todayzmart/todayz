import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { ChevronRight, Plus, Store } from 'lucide-react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { businessTone, useFieldLabels } from './labels';
import { useFieldBusinesses } from './queries';
import css from './field.module.css';

/** A-04 start: the shops the caller onboarded, newest first, and "Add a shop". Phone first. */
export function FieldPage() {
  const intl = useIntl();
  const navigate = useNavigate();
  const labels = useFieldLabels();
  const problemText = useProblemText();
  const q = useFieldBusinesses();
  const rows = q.data?.pages.flatMap((p) => p.items) ?? [];
  const add = (
    <Button variant="primary" icon={Plus} onClick={() => void navigate({ to: '/field/new' })}>
      {intl.formatMessage({ id: 'field.add' })}
    </Button>
  );
  return (
    <div className={css.page}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.field' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'field.sub' })}</p>
        </div>
        {rows.length > 0 && add}
      </div>
      {q.isPending && <Skeleton shape="block" height="160px" />}
      {q.isError && (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      )}
      {q.isSuccess && rows.length === 0 && (
        <EmptyState icon={Store} title={intl.formatMessage({ id: 'field.empty.title' })} description={intl.formatMessage({ id: 'field.empty.body' })} action={add} />
      )}
      {rows.length > 0 && (
        <ul className={css.list} aria-label={intl.formatMessage({ id: 'field.list' })}>
          {rows.map((b) => (
            <li key={b.id}>
              <Link className={css.shop} to="/field/$businessId" params={{ businessId: b.id }}>
                <span>
                  <span className={css.shopName}>{b.brandName}</span>
                  <br />
                  <span className={css.muted}>
                    {intl.formatMessage({ id: 'field.shop.counts' }, { stores: b.stores.length, items: b.stores.reduce((n, s) => n + s.itemCount, 0) })}
                  </span>
                </span>
                <span className={css.head}>
                  <StatusPill tone={businessTone(b.status)}>{labels.status(b.status)}</StatusPill>
                  <ChevronRight size={16} aria-hidden="true" />
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {q.hasNextPage && (
        <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {intl.formatMessage({ id: 'common.loadMore' })}
        </Button>
      )}
    </div>
  );
}
