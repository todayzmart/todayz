import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Inbox, Store as StoreIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { canSeeInsights } from './queries';
import styles from './overview.module.css';

/**
 * The current store for the owner-and-manager screens (B-03 Overview, B-12 Analytics, B-09
 * Customers). A cashier gets a note with the screens they use instead, and no call is made, so they
 * never meet a 403 (06 "Role limits in the UI").
 */
export function StoreGate({ next, children }: { next: string; children: (store: CurrentStore) => ReactNode }) {
  const intl = useIntl();
  const current = useCurrentStore();
  if (current.status === 'loading') {
    return (
      <div className={styles.page} role="status" aria-label={intl.formatMessage({ id: 'overview.loading' })}>
        <Skeleton shape="block" height="120px" />
      </div>
    );
  }
  if (current.status === 'error') {
    return (
      <div className={styles.page}>
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'overview.gate.error' })}
          action={<Button loading={current.retrying} onClick={current.retry}>{intl.formatMessage({ id: 'overview.retry' })}</Button>}
        />
      </div>
    );
  }
  if (current.status === 'none') {
    return (
      <div className={styles.page}>
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'overview.gate.noStore' })}
          action={
            <Link to="/choose-shop" search={{ next }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'overview.gate.choose' })}
            </Link>
          }
        />
      </div>
    );
  }
  if (!canSeeInsights(current.store.role)) {
    return (
      <div className={styles.page}>
        <Banner
          tone="info"
          title={intl.formatMessage({ id: 'overview.gate.cashier' })}
          action={
            <span className={styles.gateLinks}>
              <Link to="/orders" className="tz-btn tz-btn-sm">
                {intl.formatMessage({ id: 'overview.gate.orders' })}
              </Link>
              <Link to="/sell" className="tz-btn tz-btn-sm tz-btn-primary">
                {intl.formatMessage({ id: 'overview.newSale' })}
              </Link>
            </span>
          }
        />
      </div>
    );
  }
  return <>{children(current.store)}</>;
}
