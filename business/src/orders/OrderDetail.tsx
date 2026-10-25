import { ApiError } from '@todayz/api-client';
import type { OrderStatus } from '@todayz/api-client/gen/model/orderStatus';
import type { StoreOrder } from '@todayz/api-client/gen/model/storeOrder';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, EmptyState, OrderTimeline, Skeleton, StatusPill, orderStatusTone, useToast, type TimelineStep } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, Bike, Inbox, ShoppingBag } from 'lucide-react';
import { useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { useAcknowledgeOnOpen } from './alerts';
import { ActionError } from './ActionError';
import { CodeEntry } from './CodeEntry';
import { AcceptPartSheet } from './AcceptPartSheet';
import { canAcceptPart, clock, isShort, lineName, reasonLabel, statusLabel } from './labels';
import { acceptCall, acceptPartCall, cancelCall, dispatchCall, failDeliveryCall, readyCall, rejectCall, useOrder, useOrderAction } from './queries';
import { ReasonSheet } from './ReasonSheet';
import { ReturnPanel } from './ReturnPanel';
import { canCancelAccepted, useCurrentStore, type CurrentStore } from './store';
import styles from './orders.module.css';

const REJECT_REASONS = ['out_of_stock', 'closing_soon', 'too_busy', 'cannot_deliver_address', 'price_changed', 'other'] as const;
const CANCEL_REASONS = ['out_of_stock', 'customer_unreachable', 'closing_soon', 'other'] as const;
const DELIVERY_FAILED_REASONS = ['customer_unreachable', 'address_not_found', 'customer_refused', 'other'] as const;
const ENDED: readonly OrderStatus[] = ['rejected', 'expired', 'cancelled', 'delivery_failed'];

/** Steps from the order's own timeline: what happened, then what is still to come. */
function buildSteps(intl: IntlShape, order: StoreOrder): TimelineStep[] {
  const at = (s: OrderStatus) => order.timeline.find((e) => e.toStatus === s)?.at;
  const detail = (iso: string | undefined) => (iso ? clock(iso) : undefined);
  const steps: TimelineStep[] = [
    { key: 'placed', label: intl.formatMessage({ id: 'orders.detail.step.placed' }), state: 'done', detail: clock(order.placedAt) },
  ];
  const ended = order.timeline.find((e) => ENDED.includes(e.toStatus));
  const reached = (s: OrderStatus) => order.timeline.some((e) => e.toStatus === s);
  const step = (key: OrderStatus, id: string): TimelineStep => ({
    key,
    label: intl.formatMessage({ id }),
    state: reached(key) ? (order.status === key && !ended ? 'now' : 'done') : 'upcoming',
    detail: detail(at(key)),
    icon: key === 'ready' ? ShoppingBag : key === 'out_for_delivery' ? Bike : undefined,
  });
  steps.push(step('accepted', 'orders.detail.step.accepted'));
  if (order.fulfilment === 'delivery') {
    if (!ended || reached('out_for_delivery')) steps.push(step('out_for_delivery', 'orders.detail.step.out_for_delivery'));
    if (!ended) steps.push(step('delivered', 'orders.detail.step.delivered'));
  } else {
    if (!ended || reached('ready')) steps.push(step('ready', 'orders.detail.step.ready'));
    if (!ended) steps.push(step('collected', 'orders.detail.step.collected'));
  }
  if (ended) {
    steps.push({
      key: ended.toStatus,
      label: intl.formatMessage({ id: 'orders.detail.step.ended' }, { status: statusLabel(intl, ended.toStatus) }),
      state: 'failed',
      detail: [clock(ended.at), ended.reason ? reasonLabel(intl, ended.reason) : null].filter(Boolean).join(' · '),
    });
  }
  return steps;
}

/** B-04 order detail: progress, customer, items, and the actions this role may take. */
export function OrderDetail({ orderId }: { orderId: string }) {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <Link to="/orders" className={`tz-btn tz-btn-ghost ${styles.back}`}>
        <ArrowLeft className="tz-ico tz-ico-sm" aria-hidden="true" />
        {intl.formatMessage({ id: 'orders.detail.back' })}
      </Link>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
          <Skeleton height="8rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'orders.error.title' })}
          description={intl.formatMessage({ id: 'orders.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'orders.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'orders.noStore.title' })}
          description={intl.formatMessage({ id: 'orders.noStore.body' })}
        />
      )}
      {current.status === 'ready' && <DetailBody store={current.store} orderId={orderId} />}
    </div>
  );
}

function DetailBody({ store, orderId }: { store: CurrentStore; orderId: string }) {
  const intl = useIntl();
  const query = useOrder(store, orderId);
  useAcknowledgeOnOpen(store.sid, orderId);
  if (query.isError) {
    const missing = query.error instanceof ApiError && query.error.status === 404;
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: missing ? 'orders.detail.notFound' : 'orders.error.title' })}
        description={missing ? undefined : intl.formatMessage({ id: 'orders.error.body' })}
        action={missing ? undefined : <Button onClick={() => void query.refetch()}>{intl.formatMessage({ id: 'orders.error.retry' })}</Button>}
      />
    );
  }
  if (!query.data) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
        <Skeleton height="12rem" />
      </div>
    );
  }
  return <OrderView store={store} order={query.data} />;
}

function OrderView({ store, order }: { store: CurrentStore; order: StoreOrder }) {
  const intl = useIntl();
  const toast = useToast();
  const action = useOrderAction(store);
  const [rejecting, setRejecting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [parting, setParting] = useState(false);
  const [failing, setFailing] = useState(false);
  const name = order.customer.name ?? intl.formatMessage({ id: 'orders.card.noName' });
  const placed = order.status === 'placed';
  // A short line makes the API refuse Accept (insufficient_stock): disable it and say why.
  const shortLines = order.lines.filter(isShort).length;
  const partable = canAcceptPart(order.lines);
  const waiting = order.status === 'awaiting_customer';
  const open = order.status === 'accepted' || order.status === 'ready';
  const delivery = order.fulfilment === 'delivery';
  const canMarkReady = order.status === 'accepted' && !delivery;
  const canDispatch = order.status === 'accepted' && delivery;
  const outForDelivery = order.status === 'out_for_delivery';
  // Pickup hands over while accepted or ready; delivery only at the door (BK-24).
  const canHandOver = delivery ? outForDelivery : open;
  const canCancel = open && canCancelAccepted(store.role);
  const addr = order.deliveryAddress;

  const announce = (id: string, o: StoreOrder | null) => {
    if (o) toast.show({ message: intl.formatMessage({ id }) });
  };

  return (
    <div className={styles.detail}>
      <div className={styles.stack}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.detail.title' }, { number: order.number })}</h1>
        <div>
          <StatusPill tone={orderStatusTone(order.status)}>{statusLabel(intl, order.status)}</StatusPill>
        </div>
        <section className="tz-card" aria-label={intl.formatMessage({ id: 'orders.detail.progress' })}>
          <OrderTimeline
            label={intl.formatMessage({ id: 'orders.detail.progress' })}
            steps={buildSteps(intl, order)}
            stateLabels={{
              done: intl.formatMessage({ id: 'orders.detail.state.done' }),
              upcoming: intl.formatMessage({ id: 'orders.detail.state.upcoming' }),
              failed: intl.formatMessage({ id: 'orders.detail.state.failed' }),
            }}
          />
        </section>
        <section className="tz-card" aria-labelledby="cust-title">
          <h2 id="cust-title" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.detail.customer' })}
          </h2>
          <p>{name}</p>
          {order.customer.phone ? (
            // The full number only while out for delivery (BK-27, D-60), so the rider can call.
            <p>
              <a href={`tel:${order.customer.phone}`}>{intl.formatMessage({ id: 'orders.detail.call' }, { phone: order.customer.phone })}</a>
            </p>
          ) : (
            order.customer.phoneMasked && (
              <p className={styles.muted}>{intl.formatMessage({ id: 'orders.detail.phone' }, { phone: order.customer.phoneMasked })}</p>
            )
          )}
          {addr && (
            <address className={styles.address} aria-label={intl.formatMessage({ id: 'orders.detail.address' })}>
              <b>{addr.label}</b>
              <span>{[addr.line1, addr.line2].filter(Boolean).join(', ')}</span>
              {addr.landmark && <span>{intl.formatMessage({ id: 'orders.detail.landmark' }, { landmark: addr.landmark })}</span>}
              <span>{addr.pincode}</span>
            </address>
          )}
        </section>
        <section className="tz-card" aria-labelledby="items-title">
          <h2 id="items-title" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.detail.items' })}
          </h2>
          <ul className={styles.lines}>
            {order.lines.map((l) => (
              <li key={l.id} className={styles.line}>
                <span>
                  {l.quantityAccepted !== null && l.quantityAccepted !== l.quantityRequested
                    ? intl.formatMessage({ id: 'orders.line.partQty' }, { name: lineName(l), got: l.quantityAccepted, wanted: l.quantityRequested })
                    : intl.formatMessage({ id: 'orders.line.qty' }, { name: lineName(l), qty: l.quantityRequested })}
                </span>
                <span className={`${styles.lineStock} ${isShort(l) ? styles.lineShort : ''}`}>
                  {isShort(l) && l.available > 0
                    ? intl.formatMessage({ id: 'orders.line.short' })
                    : intl.formatMessage({ id: 'orders.line.stock' }, { available: Math.max(0, l.available) })}
                </span>
              </li>
            ))}
          </ul>
          {delivery && (
            <div className={styles.meta}>
              <span>{intl.formatMessage({ id: 'orders.detail.deliveryFee' })}</span>
              <span>
                {order.deliveryFeePaise > 0 ? formatMoney(order.deliveryFeePaise, 'dashboard') : intl.formatMessage({ id: 'orders.detail.deliveryFree' })}
              </span>
            </div>
          )}
          <div className={styles.meta}>
            <b>{intl.formatMessage({ id: 'orders.detail.total' })}</b>
            <span className="tz-price">{formatMoney(order.totalPaise, 'dashboard')}</span>
          </div>
          {order.paymentMethod ? (
            <p className={styles.muted}>
              {intl.formatMessage({ id: 'orders.detail.paidWith' }, { method: intl.formatMessage({ id: `orders.code.pay.${order.paymentMethod}` }) })}
            </p>
          ) : (
            <p className={styles.muted}>{intl.formatMessage({ id: delivery ? 'orders.detail.payAtDoor' : 'orders.detail.payAtShop' })}</p>
          )}
          {order.holdUntil && open && (
            <p className={styles.muted}>{intl.formatMessage({ id: 'orders.detail.holdUntil' }, { time: clock(order.holdUntil) })}</p>
          )}
        </section>
      </div>
      <div className={styles.stack}>
        {placed && shortLines > 0 && (
          <Banner tone="warning" id="short-hint">
            {intl.formatMessage({ id: partable ? 'orders.card.partHint' : 'orders.card.shortHint' }, { count: shortLines })}
          </Banner>
        )}
        {waiting && <Banner tone="info">{intl.formatMessage({ id: 'orders.part.waiting' }, { customer: name })}</Banner>}
        <ActionError error={action.error} />
        {placed && (
          <div className={styles.actions}>
            <Button onClick={() => setRejecting(true)} disabled={action.pending !== null}>
              {intl.formatMessage({ id: 'orders.card.reject' })}
            </Button>
            <Button
              onClick={() => setParting(true)}
              disabled={!partable || action.pending !== null}
              aria-describedby={shortLines > 0 && !partable ? 'short-hint' : undefined}
            >
              {intl.formatMessage({ id: 'orders.card.acceptPart' })}
            </Button>
            <Button
              variant="primary"
              loading={action.pending === 'accept'}
              disabled={shortLines > 0 || (action.pending !== null && action.pending !== 'accept')}
              aria-describedby={shortLines > 0 ? 'short-hint' : undefined}
              onClick={() => void action.run('accept', acceptCall(store, order.id)).then((o) => announce('orders.action.accepted', o))}
            >
              {intl.formatMessage({ id: 'orders.card.accept' })}
            </Button>
          </div>
        )}
        {canMarkReady && (
          <div className={styles.stack}>
            <Button
              variant="primary"
              loading={action.pending === 'ready'}
              onClick={() => void action.run('ready', readyCall(store, order.id)).then((o) => announce('orders.action.ready', o))}
            >
              {intl.formatMessage({ id: 'orders.detail.ready' })}
            </Button>
            <p className={styles.muted}>{intl.formatMessage({ id: 'orders.detail.readyHelp' })}</p>
          </div>
        )}
        {canDispatch && (
          <div className={styles.stack}>
            <Button
              variant="primary"
              loading={action.pending === 'dispatch'}
              onClick={() => void action.run('dispatch', dispatchCall(store, order.id)).then((o) => announce('orders.action.dispatched', o))}
            >
              {intl.formatMessage({ id: 'orders.detail.dispatch' })}
            </Button>
            {order.dispatchBy && (
              <p className={styles.muted}>{intl.formatMessage({ id: 'orders.detail.dispatchBy' }, { time: clock(order.dispatchBy) })}</p>
            )}
          </div>
        )}
        {canHandOver && <CodeEntry store={store} order={order} />}
        {outForDelivery && (
          <div className={styles.actions}>
            <Button variant="danger" onClick={() => setFailing(true)} disabled={action.pending !== null}>
              {intl.formatMessage({ id: 'orders.failed.open' })}
            </Button>
          </div>
        )}
        {order.status === 'delivery_failed' && <ReturnPanel store={store} order={order} />}
        {canCancel && (
          <div className={styles.actions}>
            <Button variant="danger" onClick={() => setCancelling(true)} disabled={action.pending !== null}>
              {intl.formatMessage({ id: 'orders.cancel.open' })}
            </Button>
          </div>
        )}
      </div>
      {parting && (
        <AcceptPartSheet
          order={order}
          onClose={() => setParting(false)}
          busy={action.pending === 'acceptPart'}
          error={action.error}
          onConfirm={(lines) =>
            void action.run('acceptPart', acceptPartCall(store, order.id, lines)).then((o) => {
              if (o) setParting(false);
              announce('orders.action.partSent', o);
            })
          }
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
        onConfirm={(reason, note) =>
          void action.run('reject', rejectCall(store, order.id, reason, note)).then((o) => {
            if (o) setRejecting(false);
            announce('orders.action.rejected', o);
          })
        }
      />
      <ReasonSheet
        open={failing}
        onClose={() => setFailing(false)}
        title={intl.formatMessage({ id: 'orders.failed.title' }, { number: order.number })}
        body={intl.formatMessage({ id: 'orders.failed.body' })}
        reasons={DELIVERY_FAILED_REASONS}
        confirmLabel={intl.formatMessage({ id: 'orders.failed.confirm' })}
        busy={action.pending === 'failDelivery'}
        error={action.error}
        onConfirm={(reason, note) =>
          void action.run('failDelivery', failDeliveryCall(store, order.id, reason, note)).then((o) => {
            if (o) setFailing(false);
            announce('orders.action.failed', o);
          })
        }
      />
      <ReasonSheet
        open={cancelling}
        onClose={() => setCancelling(false)}
        title={intl.formatMessage({ id: 'orders.cancel.title' }, { number: order.number })}
        body={intl.formatMessage({ id: 'orders.cancel.body' })}
        reasons={CANCEL_REASONS}
        confirmLabel={intl.formatMessage({ id: 'orders.cancel.confirm' })}
        busy={action.pending === 'cancel'}
        error={action.error}
        onConfirm={(reason, note) =>
          void action.run('cancel', cancelCall(store, order.id, reason, note)).then((o) => {
            if (o) setCancelling(false);
            announce('orders.action.cancelled', o);
          })
        }
      />
    </div>
  );
}
