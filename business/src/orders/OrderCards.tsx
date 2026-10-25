import type { StoreOrderSummary } from '@todayz/api-client/gen/model/storeOrderSummary';
import { formatMoney } from '@todayz/i18n';
import { Button, orderStatusTone, StatusPill, VisuallyHidden } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Clock } from 'lucide-react';
import { useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { mmss } from '../overview/format';
import { Icon } from '../shell/Icon';
import { acknowledgeOrder } from './alerts';
import { ActionError } from './ActionError';
import { AcceptPartSheet } from './AcceptPartSheet';
import { canAcceptPart, clock, isShort, lineName, reasonLabel, since, statusLabel } from './labels';
import { acceptCall, acceptPartCall, rejectCall, useNow, useOrder, useOrderAction, useSecondsLeft } from './queries';
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
 * Accept, Accept part and Reject. While a line is short, Accept is disabled with a hint (the API
 * refuses it with `insufficient_stock`): the shop accepts part (D-28), rejects or fixes the stock.
 * When the stock covers nothing, Accept part is disabled too and the hint says reject or restock.
 */
export function NewOrderCard({ store, order, offsetMs }: { store: CurrentStore; order: StoreOrderSummary; offsetMs: number }) {
  const intl = useIntl();
  const detail = useOrder(store, order.id);
  const action = useOrderAction(store);
  const [rejecting, setRejecting] = useState(false);
  const [parting, setParting] = useState(false);
  const left = useSecondsLeft(order.acceptBy, offsetMs);
  const expired = left === 0;
  const name = order.customerName ?? intl.formatMessage({ id: 'orders.card.noName' });
  const time = mmss(left ?? 0);
  const shortLines = detail.data?.lines.filter(isShort).length ?? 0;
  const partable = detail.data ? canAcceptPart(detail.data.lines) : false;
  const hintId = `${order.id}-short`;
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
      {shortLines > 0 && (
        <p id={hintId} className={styles.shortHint}>
          {intl.formatMessage({ id: partable ? 'orders.card.partHint' : 'orders.card.shortHint' }, { count: shortLines })}
        </p>
      )}
      <div className={`tz-ocard-actions ${styles.threeActions}`}>
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
          onClick={() => setParting(true)}
          disabled={!partable || action.pending !== null}
          aria-describedby={shortLines > 0 && !partable ? hintId : undefined}
          aria-label={intl.formatMessage({ id: 'orders.card.acceptPartA11y' }, { number: order.number })}
        >
          {intl.formatMessage({ id: 'orders.card.acceptPart' })}
        </Button>
        <Button
          size="sm"
          variant="primary"
          loading={action.pending === 'accept'}
          disabled={shortLines > 0 || (action.pending !== null && action.pending !== 'accept')}
          aria-describedby={shortLines > 0 ? hintId : undefined}
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
      {parting && detail.data && (
        <AcceptPartSheet
          order={detail.data}
          onClose={() => setParting(false)}
          busy={action.pending === 'acceptPart'}
          error={action.error}
          onConfirm={(lines) => {
            acknowledgeOrder(store.sid, order.id);
            void action.run('acceptPart', acceptPartCall(store, order.id, lines)).then((o) => {
              if (o) setParting(false);
            });
          }}
        />
      )}
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

/** The row's time: the hold or dispatch deadline while open, the time since a failed delivery
 * while its return waits, the return's outcome once settled, else when it was placed. */
function rowTime(intl: IntlShape, order: StoreOrderSummary, now: number): string {
  const open = order.status === 'accepted' || order.status === 'ready';
  if (open && order.fulfilment === 'delivery' && order.dispatchBy)
    return intl.formatMessage({ id: 'orders.card.dispatchBy' }, { time: clock(order.dispatchBy) });
  if (open && order.holdUntil) return intl.formatMessage({ id: 'orders.card.holdLeft' }, { time: clock(order.holdUntil) });
  if (order.status === 'delivery_failed' && order.returnOutcome)
    return intl.formatMessage({ id: `orders.return.short.${order.returnOutcome}` });
  if (order.status === 'delivery_failed' && order.deliveryFailedAt)
    return intl.formatMessage({ id: 'orders.return.failedAgo' }, { time: since(intl, order.deliveryFailedAt, now) });
  return clock(order.placedAt);
}

/** A row on every tab but New. */
export function OrderRow({ order }: { order: StoreOrderSummary }) {
  const intl = useIntl();
  const now = useNow();
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
        <span className={styles.lineStock}>{rowTime(intl, order, now)}</span>
      </div>
      {order.customerPhone && (
        // The full number only while out for delivery (BK-27), so the rider can call.
        <a href={`tel:${order.customerPhone}`} className={styles.lineStock}>
          {intl.formatMessage({ id: 'orders.detail.call' }, { phone: order.customerPhone })}
        </a>
      )}
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
