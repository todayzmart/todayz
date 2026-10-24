import type { StoreOrder } from '@todayz/api-client/gen/model/storeOrder';
import { Banner, Button, useToast } from '@todayz/ui';
import { PackageX } from 'lucide-react';
import { useIntl } from 'react-intl';
import { ActionError } from './ActionError';
import { clock, since } from './labels';
import { confirmReturnCall, useNow, useOrderAction } from './queries';
import { canRecordDamage, type CurrentStore } from './store';
import styles from './orders.module.css';

/**
 * A failed delivery's goods coming back (D-69, BK-26). Yes releases the stock (cashier and up);
 * No records damage, a stock write-off, so only a manager or the owner sees it. Unanswered, the
 * return job reminds the store and settles it unconfirmed after 72 h.
 */
export function ReturnPanel({ store, order }: { store: CurrentStore; order: StoreOrder }) {
  const intl = useIntl();
  const toast = useToast();
  const action = useOrderAction(store);
  const now = useNow();
  if (order.returnOutcome) {
    return (
      <section className={`tz-card ${styles.codeBox}`} aria-labelledby="return-title">
        <h2 id="return-title" className="tz-card-title">
          {intl.formatMessage({ id: 'orders.return.title' })}
        </h2>
        <p>{intl.formatMessage({ id: `orders.return.outcome.${order.returnOutcome}` })}</p>
        {order.returnConfirmedAt && <p className={styles.muted}>{clock(order.returnConfirmedAt)}</p>}
      </section>
    );
  }
  const run = (outcome: 'returned' | 'damaged') =>
    void action.run('confirmReturn', confirmReturnCall(store, order.id, outcome)).then((o) => {
      if (o) toast.show({ message: intl.formatMessage({ id: `orders.action.return.${outcome}` }) });
    });
  return (
    <section className={`tz-card ${styles.codeBox}`} aria-labelledby="return-title">
      <h2 id="return-title" className="tz-card-title">
        {intl.formatMessage({ id: 'orders.return.question' })}
      </h2>
      {order.deliveryFailedAt && (
        <p className={styles.muted}>
          {intl.formatMessage({ id: 'orders.return.failedAgo' }, { time: since(intl, order.deliveryFailedAt, now) })}
        </p>
      )}
      <p className={styles.muted}>{intl.formatMessage({ id: 'orders.return.help' })}</p>
      <ActionError error={action.error} />
      <div className={styles.actions}>
        <Button variant="primary" loading={action.pending === 'confirmReturn'} disabled={action.pending !== null} onClick={() => run('returned')}>
          {intl.formatMessage({ id: 'orders.return.yes' })}
        </Button>
        {canRecordDamage(store.role) && (
          <Button variant="danger" disabled={action.pending !== null} onClick={() => run('damaged')}>
            {intl.formatMessage({ id: 'orders.return.no' })}
          </Button>
        )}
      </div>
      {!canRecordDamage(store.role) && (
        <Banner tone="info" icon={PackageX} title={intl.formatMessage({ id: 'orders.return.askManager' })}>
          {intl.formatMessage({ id: 'orders.return.askManagerBody' })}
        </Banner>
      )}
    </section>
  );
}
