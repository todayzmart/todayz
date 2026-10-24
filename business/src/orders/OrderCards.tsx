import type { StoreOrderSummary } from '@todayz/api-client/gen/model/storeOrderSummary';
import { formatMoney } from '@todayz/i18n';
import { Button, orderStatusTone, StatusPill, VisuallyHidden } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Clock } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { mmss } from '../overview/format';
import { Icon } from '../shell/Icon';
import { acknowledgeOrder } from './alerts';
import { ActionError } from './ActionError';
import { clock, isShort, lineName, reasonLabel, statusLabel } from './labels';
import { acceptCall, rejectCall, useOrder, useOrderAction, useSecondsLeft } from './queries';
import { ReasonSheet } from './ReasonSheet';
import type { CurrentStore } from './store';
import styles from './orders.module.css';

const REJECT_REASONS = ['out_of_stock', 'closing_soon', 'too_busy', 'cannot_deliver_address', 'price_changed', 'other'] as const;

function FulfilmentPill({ fulfilment }: { fulfilment: StoreOrderSummary['fulfilment'] }) {
  const intl = useIntl();
  return (
    <StatusPill tone={fulfilment === 'pickup' ? 'accepted' : 'delivery'}>
      {intl.formatMessage({ id: fulfilment === 'pickup' ? 'orders.card.pickup' : 'orders.card.delivery' })}
    </StatusPill>
  );
}

/**
 * A new order: the time left to accept (from the server's clock), the items with their stock, and
 * Accept and Reject. Accept part is not here: partial accept is deferred in W2-02.
 * TODO(W2-02): "Accept part" opens a quantity editor once the API takes `lines` below the quantity asked.
 */
export function NewOrderCard({ store, order, offsetMs }: { store: CurrentStore; order: StoreOrderSummary; offsetMs: number }) {
  const intl = useIntl();
  const detail = useOrder(store, order.id);
  const action = useOrderAction(store);
  const [rejecting, setRejecting] = useState(false);
  const left = useSecondsLeft(order.acceptBy, offsetMs);
  const expired = left === 0;
  const name = order.customerName ?? intl.formatMessage({ id: 'orders.card.noName' });
  const time = mmss(left ?? 0);
  return (
    <article className="tz-ocard" aria-label={order.number}>
      <div className="tz-ocard-top">
        <span>{intl.formatMessage({ id: 'orders.card.head' }, { number: order.number, customer: name })}</span>
        {left !== null && (
          <span className="tz-timer" data-testid="countdown">
            <Icon icon={Clock} className="tz-ico-sm" />
            <span aria-hidden="true">{time}</span>
            <VisuallyHidden>
              {expired ? intl.formatMessage({ id: 'orders.card.expired' }) : intl.formatMessage({ id: 'orders.card.timeLeft' }, { time })}
            </VisuallyHidden>
          </span>
        )}
      </div>
      {detail.data ? (
        <ul className={styles.lines}>
          {detail.data.lines.map((l) => (
            <li key={l.id} className={styles.line}>
              <span>{intl.formatMessage({ id: 'orders.line.qty' }, { name: lineName(l), qty: l.quantityRequested })}</span>
              <span className={`${styles.lineStock} ${isShort(l) ? styles.lineShort : ''}`}>
                {isShort(l) && l.available > 0
                  ? intl.formatMessage({ id: 'orders.line.short' })
                  : intl.formatMessage({ id: 'orders.line.stock' }, { available: Math.max(0, l.available) })}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <div className={styles.summary}>{intl.formatMessage({ id: 'orders.card.items' }, { count: order.itemCount })}</div>
      )}
      <div className={styles.meta}>
        <span className="tz-price">{formatMoney(order.totalPaise, 'dashboard')}</span>
        <FulfilmentPill fulfilment={order.fulfilment} />
      </div>
      <ActionError error={action.error} />
      <div className="tz-ocard-actions">
        <Button
          size="sm"
          onClick={() => setRejecting(true)}
          disabled={action.pending !== null}
          aria-label={intl.formatMessage({ id: 'orders.card.rejectA11y' }, { number: order.number })}
        >
          {intl.formatMessage({ id: 'orders.card.reject' })}
        </Button>
        <Button
          size="sm"
          variant="primary"
          loading={action.pending === 'accept'}
          disabled={action.pending !== null && action.pending !== 'accept'}
          onClick={() => {
            acknowledgeOrder(store.sid, order.id);
            void action.run('accept', acceptCall(store, order.id));
          }}
          aria-label={intl.formatMessage({ id: 'orders.card.acceptA11y' }, { number: order.number })}
        >
          {intl.formatMessage({ id: 'orders.card.accept' })}
        </Button>
      </div>
      <Link
        to="/orders/$orderId"
        params={{ orderId: order.id }}
        className={`tz-btn tz-btn-ghost tz-btn-sm ${styles.cardLink}`}
        onClick={() => acknowledgeOrder(store.sid, order.id)}
        aria-label={intl.formatMessage({ id: 'orders.card.openA11y' }, { number: order.number })}
      >
        {intl.formatMessage({ id: 'orders.card.open' })}
      </Link>
      <ReasonSheet
        open={rejecting}
        onClose={() => setRejecting(false)}
        title={intl.formatMessage({ id: 'orders.reject.title' }, { number: order.number })}
        body={intl.formatMessage({ id: 'orders.reject.body' })}
        reasons={REJECT_REASONS}
        confirmLabel={intl.formatMessage({ id: 'orders.reject.confirm' })}
        busy={action.pending === 'reject'}
        error={action.error}
        onConfirm={(reason, note) => {
          acknowledgeOrder(store.sid, order.id);
          void action.run('reject', rejectCall(store, order.id, reason, note)).then((o) => {
            if (o) setRejecting(false);
          });
        }}
      />
    </article>
  );
}

/** A row on the Accepted, Ready, Done and Missed tabs. */
export function OrderRow({ order }: { order: StoreOrderSummary }) {
  const intl = useIntl();
  const name = order.customerName ?? intl.formatMessage({ id: 'orders.card.noName' });
  return (
    <article className="tz-ocard" aria-label={order.number}>
      <div className="tz-ocard-top">
        <span>{intl.formatMessage({ id: 'orders.card.head' }, { number: order.number, customer: name })}</span>
        <StatusPill tone={orderStatusTone(order.status)}>{statusLabel(intl, order.status)}</StatusPill>
      </div>
      <div className={styles.summary}>
        {intl.formatMessage({ id: 'orders.card.items' }, { count: order.itemCount })}
        {order.statusReason ? ` · ${intl.formatMessage({ id: 'orders.reason.label' }, { reason: reasonLabel(intl, order.statusReason) })}` : ''}
      </div>
      <div className={styles.meta}>
        <span className="tz-price">{formatMoney(order.totalPaise, 'dashboard')}</span>
        <span className={styles.lineStock}>
          {order.holdUntil && (order.status === 'accepted' || order.status === 'ready')
            ? intl.formatMessage({ id: 'orders.card.holdLeft' }, { time: clock(order.holdUntil) })
            : clock(order.placedAt)}
        </span>
      </div>
      <Link
        to="/orders/$orderId"
        params={{ orderId: order.id }}
        className={`tz-btn tz-btn-ghost tz-btn-sm ${styles.cardLink}`}
        aria-label={intl.formatMessage({ id: 'orders.card.openA11y' }, { number: order.number })}
      >
        {intl.formatMessage({ id: 'orders.card.open' })}
      </Link>
    </article>
  );
}
