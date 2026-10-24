import { Link } from '@tanstack/react-router';
import { Button, EmptyState, Skeleton } from '@todayz/ui';
import { Inbox, Store as StoreIcon } from 'lucide-react';
import { useIntl } from 'react-intl';
import { Counter } from './Counter';
import { PosProvider } from './runtime';
import { usePosStore } from './store';
import styles from './pos.module.css';

/** B-05 Sell: gates on the current shop, then runs the counter for it. */
export function SellPage() {
  const intl = useIntl();
  const current = usePosStore();
  if (current.status === 'ready') {
    const { store } = current;
    return (
      <PosProvider
        store={store}
        fallback={
          <div role="status" aria-label={intl.formatMessage({ id: 'pos.loading' })}>
            <Skeleton height="8rem" />
          </div>
        }
      >
        <Counter />
      </PosProvider>
    );
  }
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'pos.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'pos.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'pos.error.title' })}
          description={intl.formatMessage({ id: 'pos.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'pos.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'pos.noStore.title' })}
          description={intl.formatMessage({ id: 'pos.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/sell' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'pos.noStore.action' })}
            </Link>
          }
        />
      )}
    </div>
  );
}
