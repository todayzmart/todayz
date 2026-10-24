// C-12 partial accept (`awaiting_customer`, D-28): the shop could not give everything. The customer sees
// the changed lines, the new items total, credits, fee and total (the server re-capped credits and recomputed the delivery fee) and takes
// it or cancels it before `partialConfirmBy`. Cancel records `declined_partial` on the server.
import { ApiError } from '@todayz/api-client';
import type { Order } from '@todayz/api-client/gen/model/order';
import { confirmPartialOrder } from '@todayz/api-client/gen/orders/orders';
import { formatMoney } from '@todayz/i18n';
import { useMutation } from '@tanstack/react-query';
import { Banner, Button } from '@todayz/ui';
import { Clock } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import styles from '../cart/tx.module.css';
import { Icon } from '../shell/Icon';
import { formatMmSs } from './clock';

export function PartialConfirm({
  order,
  left,
  keyFor,
  done,
  onNext,
}: {
  order: Order;
  /** Seconds to `partialConfirmBy`, from the server's clock. */
  left: number | null;
  keyFor: (name: string) => string;
  done: (name: string) => void;
  onNext: (o: Order) => void;
}) {
  const intl = useIntl();
  const [problem, setProblem] = useState<'failed' | 'late' | null>(null);
  const answer = useMutation({
    mutationFn: (accept: boolean) => confirmPartialOrder(order.id, { accept }, { idempotencyKey: keyFor(`partial:${accept}`) }),
    onSuccess: (next, accept) => {
      done(`partial:${accept}`);
      onNext(next);
    },
    onError: (err, accept) => {
      done(`partial:${accept}`);
      // The window closed or the stock moved again: the server's order decides what the page shows.
      setProblem(err instanceof ApiError && err.status === 409 ? 'late' : 'failed');
    },
  });

  const changed = order.lines.filter((l) => (l.quantityAccepted ?? l.quantityRequested) !== l.quantityRequested);
  const over = left !== null && left <= 0;

  return (
    <section className={`tz-card ${styles.stack}`} aria-live="polite" aria-label={intl.formatMessage({ id: 'orders.partial.title' }, { shop: order.store.name })}>
      <h2 className="tz-card-title">{intl.formatMessage({ id: 'orders.partial.title' }, { shop: order.store.name })}</h2>
      <p className={styles.meta}>{intl.formatMessage({ id: 'orders.partial.body' }, { shop: order.store.name })}</p>
      <ul className={styles.list}>
        {changed.map((l) => {
          const got = l.quantityAccepted ?? l.quantityRequested;
          return (
            <li key={l.id} className={styles.line}>
              <div>
                <p className={styles.name}>{l.name}</p>
                <p className={styles.flag}>
                  {got === 0
                    ? intl.formatMessage({ id: 'orders.partial.none' }, { wanted: l.quantityRequested })
                    : intl.formatMessage({ id: 'orders.partial.fewer' }, { wanted: l.quantityRequested, got })}
                </p>
              </div>
              <span className={styles.price}>{formatMoney(l.lineTotalPaise)}</span>
            </li>
          );
        })}
      </ul>
      <dl className={styles.totals}>
        <div className={styles.totalRow}>
          <dt>{intl.formatMessage({ id: 'orders.partial.items' })}</dt>
          <dd>{formatMoney(order.itemsAfterOffersPaise)}</dd>
        </div>
        {order.creditsUsed > 0 && (
          <div className={styles.totalRow}>
            <dt>{intl.formatMessage({ id: 'orders.partial.credits' })}</dt>
            <dd>−{formatMoney(order.creditsUsed * 100)}</dd>
          </div>
        )}
        {order.fulfilment === 'delivery' && (
          <div className={styles.totalRow}>
            <dt>{intl.formatMessage({ id: 'orders.partial.fee' })}</dt>
            <dd>{formatMoney(order.deliveryFeePaise)}</dd>
          </div>
        )}
        <div className={`${styles.totalRow} ${styles.grand}`}>
          <dt>{intl.formatMessage({ id: 'orders.partial.total' })}</dt>
          <dd>{formatMoney(order.totalPaise)}</dd>
        </div>
      </dl>
      {left !== null && (
        <p className={styles.countdown} aria-live="off">
          <Icon icon={Clock} />{' '}
          {left > 0
            ? intl.formatMessage({ id: 'orders.partial.left' }, { left: formatMmSs(left) })
            : intl.formatMessage({ id: 'orders.partial.over' })}
        </p>
      )}
      {problem && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: problem === 'late' ? 'orders.partial.late' : 'orders.actionError' })}
        </Banner>
      )}
      <div className={styles.actions}>
        <Button
          variant="brand"
          loading={answer.isPending && answer.variables === true}
          disabled={over || answer.isPending}
          onClick={() => answer.mutate(true)}
        >
          {intl.formatMessage({ id: 'orders.partial.confirm' }, { total: formatMoney(order.totalPaise) })}
        </Button>
        <Button
          variant="soft-danger"
          loading={answer.isPending && answer.variables === false}
          disabled={over || answer.isPending}
          onClick={() => answer.mutate(false)}
        >
          {intl.formatMessage({ id: 'orders.partial.decline' })}
        </Button>
      </div>
    </section>
  );
}
