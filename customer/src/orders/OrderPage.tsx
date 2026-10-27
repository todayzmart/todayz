import { ApiError } from '@todayz/api-client';
import type { CustomerCancelReason } from '@todayz/api-client/gen/model/customerCancelReason';
import type { Order } from '@todayz/api-client/gen/model/order';
import { getDiscoverStore } from '@todayz/api-client/gen/discover/discover';
import { cancelOrder, extendOrder, getMyOrder, newOrderCode } from '@todayz/api-client/gen/orders/orders';
import { formatClock, formatMoney } from '@todayz/i18n';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import {
  Banner,
  Button,
  OrderTimeline,
  PickupCode,
  Sheet,
  Skeleton,
  StatusPill,
  TextField,
  orderStatusTone,
  type TimelineStep,
} from '@todayz/ui';
import { Check, Clock, MapPin, Phone } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { LineThumb } from '../cart/LineThumb';
import styles from '../cart/tx.module.css';
import { NextFromSplit } from '../basket/NextFromSplit';
import { PartialConfirm } from './PartialConfirm';
import { Icon } from '../shell/Icon';
import { formatMmSs, useServerCountdown } from './clock';
import { PathLink } from './ShopLink';
import { addressLine } from '../address/line';
import { useMyOrderStream } from './stream';
import { newUlid } from './ulid';

const PICKUP_PATH = ['placed', 'accepted', 'ready', 'collected'] as const;
const DELIVERY_PATH = ['placed', 'accepted', 'out_for_delivery', 'delivered'] as const;
const FAILED = new Set(['rejected', 'expired', 'cancelled', 'delivery_failed']);
const CANCELLABLE = new Set(['placed', 'accepted', 'ready']);

function stepIndex(status: string): number {
  switch (status) {
    case 'accepted':
      return 1;
    case 'ready':
    case 'out_for_delivery':
      return 2;
    case 'collected':
    case 'delivered':
      return 3;
    default:
      return 0;
  }
}

function orderSteps(order: Order, intl: IntlShape): TimelineStep[] {
  const at = (status: string) => {
    const e = order.timeline.find((t) => t.toStatus === status);
    return e ? formatClock(e.at) : undefined;
  };
  const done = (key: string, label: string, state: TimelineStep['state']): TimelineStep => {
    const detail = at(key);
    return { key, label, state, ...(detail ? { detail } : {}) };
  };
  if (FAILED.has(order.status)) {
    const failed = done(order.status, intl.formatMessage({ id: `orders.status.${order.status}` }), 'failed');
    return [done('placed', intl.formatMessage({ id: 'orders.step.placed' }), 'done'), failed];
  }
  const now = stepIndex(order.status);
  const path = order.fulfilment === 'delivery' ? DELIVERY_PATH : PICKUP_PATH;
  const last = path.length - 1;
  return path.map((key, i) =>
    done(key, intl.formatMessage({ id: `orders.step.${key}` }), i < now ? 'done' : i === now ? (i === last ? 'done' : 'now') : 'upcoming'),
  );
}

/** Reasons the customer can give (D-60). `declined_partial` is set only by the partial-confirm screen. */
const REASONS: readonly CustomerCancelReason[] = ['changed_mind', 'too_slow', 'other'];

/** C-12 Order placed / tracking. */
export function OrderPage({ orderId, placed }: { orderId: string; placed: boolean }) {
  const intl = useIntl();
  const router = useRouter();
  useMyOrderStream();

  // The check animation plays once: the flag leaves the URL straight away.
  const [showCheck] = useState(placed);
  useEffect(() => {
    if (placed) router.history.replace(`/orders/${orderId}`);
  }, [placed, orderId, router]);

  const query = useQuery({ queryKey: ['order', orderId], queryFn: () => getMyOrder(orderId), retry: 1 });

  if (query.isPending) {
    return (
      <div className={styles.page}>
        <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
          <Skeleton height="8rem" />
        </div>
      </div>
    );
  }
  if (query.isError || !query.data) {
    return (
      <div className={styles.page} role="alert">
        <p>{intl.formatMessage({ id: 'orders.loadError' })}</p>
        <Button onClick={() => void query.refetch()}>{intl.formatMessage({ id: 'orders.retry' })}</Button>
      </div>
    );
  }
  return <OrderView order={query.data} showCheck={showCheck} />;
}

function OrderView({ order, showCheck }: { order: Order; showCheck: boolean }) {
  const intl = useIntl();
  const client = useQueryClient();
  const status = order.status;
  const shop = order.store.name;
  const delivery = order.fulfilment === 'delivery';
  const open = status === 'accepted' || status === 'ready' || status === 'out_for_delivery';

  // acceptBy is the shop's answer deadline; holdUntil is how long an accepted order is kept.
  const acceptLeft = useServerCountdown(status === 'placed' ? order.acceptBy : null, order.serverNow);
  const holdLeft = useServerCountdown(open ? order.holdUntil : null, order.serverNow);
  const partialLeft = useServerCountdown(status === 'awaiting_customer' ? order.partialConfirmBy : null, order.serverNow);

  // When a countdown runs out the server decides what happens: ask it once.
  const expiredFor = useRef<string | null>(null);
  useEffect(() => {
    const out = (status === 'placed' && acceptLeft === 0) || (open && holdLeft === 0) || (status === 'awaiting_customer' && partialLeft === 0);
    const key = `${status}:${order.version}`;
    if (out && expiredFor.current !== key) {
      expiredFor.current = key;
      void client.invalidateQueries({ queryKey: ['order', order.id] });
    }
  }, [status, acceptLeft, holdLeft, partialLeft, open, order.version, order.id, client]);

  const store = useQuery({
    queryKey: ['store-page', order.store.slug],
    enabled: open && !delivery,
    queryFn: () => getDiscoverStore(order.store.slug),
    staleTime: 5 * 60_000,
    retry: false,
  });

  // One Idempotency-Key per action attempt; a retry after a dropped connection reuses it.
  const keys = useRef(new Map<string, string>());
  const [notice, setNotice] = useState<'extended' | 'extension_used' | 'failed' | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);

  const apply = (next: Order) => client.setQueryData(['order', order.id], next);
  const keyFor = (name: string) => {
    let k = keys.current.get(name);
    if (!k) {
      k = newUlid();
      keys.current.set(name, k);
    }
    return k;
  };
  const done = (name: string) => keys.current.delete(name);

  const extend = useMutation({
    mutationFn: () => extendOrder(order.id, { idempotencyKey: keyFor('extend') }),
    onSuccess: (next) => {
      done('extend');
      apply(next);
      setNotice('extended');
    },
    onError: (err) => {
      done('extend');
      setNotice(err instanceof ApiError && err.code === 'extension_already_used' ? 'extension_used' : 'failed');
      void client.invalidateQueries({ queryKey: ['order', order.id] });
    },
  });
  const freshCode = useMutation({
    mutationFn: () => newOrderCode(order.id, { idempotencyKey: keyFor('code') }),
    onSuccess: () => {
      done('code');
      // The action answer never carries the code: read the order for it.
      void client.invalidateQueries({ queryKey: ['order', order.id] });
    },
    onError: () => setNotice('failed'),
  });

  const lines = order.lines;
  // Delivery: the code is for the rider, so it shows once the order is on its way (01 §5.5).
  const showCode = open && order.code !== null && !order.codeLocked && (!delivery || status === 'out_for_delivery');
  const where = !delivery && store.data ? `https://www.google.com/maps/dir/?api=1&destination=${store.data.lat},${store.data.lng}` : null;

  return (
    <div className={styles.page}>
      <header className={styles.stack}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.detail.title' }, { number: order.number })}</h1>
        <p className={styles.meta}>
          {shop} · <StatusPill tone={orderStatusTone(status)}>{intl.formatMessage({ id: `orders.status.${status}` })}</StatusPill>
        </p>
      </header>

      {showCheck && (
        <div className={styles.hero}>
          <span className={styles.check} data-testid="placed-check">
            <Icon icon={Check} />
          </span>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'orders.placed.title' })}</h2>
        </div>
      )}

      {status === 'placed' && (
        <section className={`tz-card ${styles.stack}`} aria-live="polite">
          <p className={styles.countdown}>
            {acceptLeft !== null && acceptLeft > 0
              ? intl.formatMessage({ id: 'orders.waiting' }, { shop, left: formatMmSs(acceptLeft) })
              : intl.formatMessage({ id: 'orders.waitingLong' }, { shop })}
          </p>
          <p className={styles.meta}>{intl.formatMessage({ id: 'orders.waiting.note' })}</p>
          <CallShop order={order} />
        </section>
      )}

      {status === 'awaiting_customer' && (
        <PartialConfirm
          order={order}
          left={partialLeft}
          keyFor={keyFor}
          done={done}
          onNext={(next) => {
            apply(next);
            // Action answers never carry the pickup code (the server reveals it only on GET): read the
            // order again so an accepted change shows the code without waiting for a live update.
            void client.invalidateQueries({ queryKey: ['order', order.id] });
            void client.invalidateQueries({ queryKey: ['orders'] });
          }}
        />
      )}
      {status === 'awaiting_customer' && <CallShop order={order} card />}

      {open && (
        <section className={`tz-card ${styles.stack}`}>
          {showCode && order.code ? (
            <PickupCode
              label={intl.formatMessage({ id: delivery ? 'orders.code.deliveryLabel' : 'orders.code.label' })}
              code={order.code}
              note={delivery ? intl.formatMessage({ id: 'orders.code.riderNote' }) : intl.formatMessage({ id: 'orders.code.note' }, { shop })}
            >
              {/* TODO(lead): a QR of the code needs a QR library; none is in the workspace. Digits only. */}
              <span className="tz-ui-code-note">{intl.formatMessage({ id: 'orders.code.brightness' })}</span>
            </PickupCode>
          ) : order.codeLocked && (!delivery || status === 'out_for_delivery') ? (
            <div className={styles.stack}>
              <Banner tone="warning" title={intl.formatMessage({ id: 'orders.code.locked.title' })}>
                {intl.formatMessage({ id: 'orders.code.locked.body' })}
              </Banner>
              <Button variant="primary" loading={freshCode.isPending} onClick={() => freshCode.mutate()}>
                {intl.formatMessage({ id: 'orders.code.new' })}
              </Button>
            </div>
          ) : null}
          {status === 'accepted' && !delivery && <p className={styles.meta}>{intl.formatMessage({ id: 'orders.accepted.note' }, { shop })}</p>}
          {status === 'accepted' && delivery && (
            <p className={styles.meta}>
              {order.dispatchBy
                ? intl.formatMessage({ id: 'orders.delivery.accepted' }, { shop, time: formatClock(order.dispatchBy) })
                : intl.formatMessage({ id: 'orders.delivery.acceptedNoTime' }, { shop })}
            </p>
          )}
          {status === 'out_for_delivery' && <p className={styles.meta}>{intl.formatMessage({ id: 'orders.delivery.onTheWay' }, { shop })}</p>}
          {status === 'ready' && <p className={styles.meta}>{intl.formatMessage({ id: 'orders.ready.note' }, { shop })}</p>}
          {holdLeft !== null && (
            <p className={styles.countdown} aria-live="off">
              <Icon icon={Clock} />{' '}
              {holdLeft > 0
                ? intl.formatMessage({ id: 'orders.hold' }, { left: formatMmSs(holdLeft) })
                : intl.formatMessage({ id: 'orders.hold.over' })}
            </p>
          )}
          {notice === 'extended' && <p role="status">{intl.formatMessage({ id: 'orders.extend.done' })}</p>}
          {notice === 'extension_used' && <p role="status">{intl.formatMessage({ id: 'orders.extend.used' })}</p>}
          {notice === 'failed' && <p role="alert">{intl.formatMessage({ id: 'orders.actionError' })}</p>}
          <div className={styles.actions}>
            {order.canExtend && (
              <Button variant="soft" loading={extend.isPending} onClick={() => extend.mutate()}>
                {intl.formatMessage({ id: 'orders.extend' })}
              </Button>
            )}
            {where && (
              <a className="tz-btn tz-btn-soft" href={where} target="_blank" rel="noopener noreferrer">
                <Icon icon={MapPin} /> {intl.formatMessage({ id: 'orders.directions' })}
              </a>
            )}
            <CallShop order={order} bare />
          </div>
        </section>
      )}

      <NextFromSplit storeId={order.store.id} status={status} />
      {FAILED.has(status) && <Outcome order={order} />}
      {(status === 'collected' || status === 'delivered') && (
        <section className={`tz-card ${styles.stack}`}>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'orders.collected.title' })}</h2>
          <PathLink to={`/s/${order.store.slug}`} className="tz-btn tz-btn-primary">
            {intl.formatMessage({ id: 'orders.again' })}
          </PathLink>
        </section>
      )}

      <section className="tz-card">
        <OrderTimeline
          label={intl.formatMessage({ id: 'orders.timeline.label' })}
          steps={orderSteps(order, intl)}
          stateLabels={{
            done: intl.formatMessage({ id: 'orders.timeline.done' }),
            upcoming: intl.formatMessage({ id: 'orders.timeline.upcoming' }),
            failed: intl.formatMessage({ id: 'orders.timeline.failed' }),
          }}
        />
      </section>

      <section className="tz-card" aria-label={intl.formatMessage({ id: 'orders.items.label' })}>
        <ul className={styles.list}>
          {lines.map((l) => (
            <li key={l.id} className={styles.line}>
              <LineThumb name={l.name} imageKey={l.imageKey} tintKey={l.storeItemId} />
              <div>
                <p className={styles.name}>
                  {l.name} × {l.quantityAccepted ?? l.quantityRequested}
                </p>
                <p className={styles.meta}>{[l.variant, `${l.packSize} ${l.unit}`].filter(Boolean).join(' · ')}</p>
              </div>
              <span className={styles.price}>{formatMoney(l.lineTotalPaise)}</span>
            </li>
          ))}
        </ul>
        <dl className={styles.totals}>
          {delivery && (
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'delivery.feeRow' })}</dt>
              <dd>{order.deliveryFeePaise > 0 ? formatMoney(order.deliveryFeePaise) : intl.formatMessage({ id: 'delivery.free' })}</dd>
            </div>
          )}
          <div className={`${styles.totalRow} ${styles.grand}`}>
            <dt>{intl.formatMessage({ id: 'cart.total' })}</dt>
            <dd>{formatMoney(order.totalPaise)}</dd>
          </div>
        </dl>
        {order.deliveryAddress && (
          <p className={styles.meta}>
            {intl.formatMessage(
              { id: 'orders.delivery.to' },
              { label: order.deliveryAddress.label, address: addressLine(order.deliveryAddress) },
            )}
          </p>
        )}
        <p className={styles.meta}>{intl.formatMessage({ id: delivery ? 'checkout.payNote.delivery' : 'checkout.payNote' })}</p>
      </section>

      {CANCELLABLE.has(status) && (
        <Button variant="soft-danger" onClick={() => setCancelOpen(true)}>
          {intl.formatMessage({ id: 'orders.cancel' })}
        </Button>
      )}
      <PathLink to={`/help/new?orderId=${order.id}`} className="tz-btn tz-btn-ghost">
        {intl.formatMessage({ id: 'help.orderLink' })}
      </PathLink>
      <CancelSheet
        order={order}
        open={cancelOpen}
        onClose={() => setCancelOpen(false)}
        keyFor={() => keyFor('cancel')}
        onDone={(next) => {
          done('cancel');
          apply(next);
          void client.invalidateQueries({ queryKey: ['orders'] });
          setCancelOpen(false);
        }}
      />
    </div>
  );
}

function Outcome({ order }: { order: Order }) {
  const intl = useIntl();
  const reason = order.statusReason;
  const known =
    reason &&
    ['out_of_stock', 'closing_soon', 'too_busy', 'other', 'changed_mind', 'too_slow', 'no_response', 'partial_timeout', 'declined_partial'].includes(reason);
  return (
    <section className={`tz-card ${styles.stack}`}>
      <h2 className="tz-card-title">{intl.formatMessage({ id: `orders.outcome.${order.status}` }, { shop: order.store.name })}</h2>
      {known && <p className={styles.meta}>{intl.formatMessage({ id: `orders.reason.${reason}` })}</p>}
      <div className={styles.actions}>
        <PathLink to="/search" className="tz-btn tz-btn-primary">
          {intl.formatMessage({ id: 'orders.findElsewhere' })}
        </PathLink>
        <PathLink to={`/s/${order.store.slug}`} className="tz-btn tz-btn-soft">
          {intl.formatMessage({ id: 'orders.again' })}
        </PathLink>
      </div>
    </section>
  );
}

function CancelSheet({
  order,
  open,
  onClose,
  keyFor,
  onDone,
}: {
  order: Order;
  open: boolean;
  onClose: () => void;
  keyFor: () => string;
  onDone: (o: Order) => void;
}) {
  const intl = useIntl();
  const [reason, setReason] = useState<CustomerCancelReason>('changed_mind');
  const [note, setNote] = useState('');
  const [error, setError] = useState(false);
  const cancel = useMutation({
    mutationFn: () =>
      cancelOrder(
        order.id,
        { reason, ...(reason === 'other' ? { note: note.trim() } : {}) },
        { idempotencyKey: keyFor() },
      ),
    onSuccess: onDone,
    onError: () => setError(true),
  });
  const needsNote = reason === 'other' && note.trim() === '';
  return (
    <Sheet
      open={open}
      onClose={onClose}
      closeLabel={intl.formatMessage({ id: 'orders.close' })}
      title={intl.formatMessage({ id: 'orders.cancel.title' })}
      description={intl.formatMessage({ id: 'orders.cancel.body' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'orders.cancel.keep' })}
          </Button>
          <Button variant="danger" loading={cancel.isPending} disabled={needsNote} onClick={() => cancel.mutate()}>
            {intl.formatMessage({ id: 'orders.cancel.confirm' })}
          </Button>
        </>
      }
    >
      <fieldset className={styles.reasons}>
        <legend className={styles.sectionTitle}>{intl.formatMessage({ id: 'orders.cancel.why' })}</legend>
        {REASONS.map((r) => (
          <label key={r} className={styles.reason}>
            <input type="radio" name="cancel-reason" checked={reason === r} onChange={() => setReason(r)} />
            {intl.formatMessage({ id: `orders.cancel.reason.${r}` })}
          </label>
        ))}
      </fieldset>
      {reason === 'other' && (
        <TextField
          label={intl.formatMessage({ id: 'orders.cancel.note' })}
          value={note}
          maxLength={200}
          onChange={(e) => setNote(e.target.value)}
        />
      )}
      {error && <p role="alert">{intl.formatMessage({ id: 'orders.cancel.error' })}</p>}
    </Sheet>
  );
}

/** "Call shop" (D-220): the server sends the shop's phone only on the customer's GET of an open order. */
function CallShop({ order, bare, card }: { order: Order; bare?: boolean; card?: boolean }) {
  const intl = useIntl();
  const phone = order.store.phone;
  if (!phone) return null;
  const link = (
    <a className="tz-btn tz-btn-soft" href={`tel:${phone}`}>
      <Icon icon={Phone} /> {intl.formatMessage({ id: 'orders.callShop' })}
    </a>
  );
  if (bare) return link;
  if (card) return <section className={`tz-card ${styles.actions}`}>{link}</section>;
  return <div className={styles.actions}>{link}</div>;
}
