import { formatMoney } from '@todayz/i18n';
import { Button, StatusPill, VisuallyHidden } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Clock } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { ActionError } from '../orders/ActionError';
import { acceptCall, useOrderAction } from '../orders/queries';
import type { CurrentStore } from '../orders/store';
import { mmss } from './format';
import { overviewKey } from './queries';
import styles from './overview.module.css';
import type { NewOrder } from './types';

/** Seconds since `since` changed (a new answer from the server), ticking once a second. */
function useElapsedSeconds(since: unknown): number {
  const [anchor, setAnchor] = useState(() => ({ since, start: Date.now() }));
  const [now, setNow] = useState(anchor.start);
  // A new answer restarts the count (state adjusted during render, not in an effect).
  if (anchor.since !== since) setAnchor({ since, start: now });
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return Math.max(0, Math.floor((now - anchor.start) / 1000));
}

/**
 * New orders with the time left to accept (server clock). Accept is the same call as the Orders
 * section (B-04, one Idempotency-Key per attempt); Reject opens the order, where the reason is chosen.
 */
export function NewOrders({ store, orders }: { store: CurrentStore; orders: readonly NewOrder[] }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const action = useOrderAction(store);
  const [acting, setActing] = useState<string | null>(null);
  const [lastId, setLastId] = useState<string | null>(null);
  const elapsed = useElapsedSeconds(orders);
  const accept = async (id: string) => {
    setActing(id);
    const done = await action.run('accept', acceptCall(store, id));
    setActing(null);
    if (done) await qc.invalidateQueries({ queryKey: overviewKey(store.sid) });
  };
  const reject = (id: string) => void navigate({ to: '/orders/$orderId', params: { orderId: id } });
  return (
    <section className={`tz-card ${styles.orders}`} aria-labelledby="ov-orders-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="ov-orders-title">
          {intl.formatMessage({ id: 'overview.orders.title' })}
        </h2>
        <StatusPill tone="pending">{intl.formatMessage({ id: 'overview.orders.waiting' }, { count: orders.length })}</StatusPill>
      </div>
      {orders.map((o) => {
        const left = o.secondsToAccept === null ? null : Math.max(0, o.secondsToAccept - elapsed);
        const time = left === null ? '' : mmss(left);
        return (
          <article key={o.id} className="tz-ocard" aria-label={o.code}>
            <div className="tz-ocard-top">
              <span>{intl.formatMessage({ id: 'overview.orders.head' }, { code: o.code, customer: o.customer })}</span>
              {left !== null && (
                <span className="tz-timer" data-testid="countdown">
                  <Clock className="tz-ico tz-ico-sm" aria-hidden="true" />
                  <span aria-hidden="true">{time}</span>
                  <VisuallyHidden>
                    {left > 0
                      ? intl.formatMessage({ id: 'overview.orders.timeLeft' }, { time })
                      : intl.formatMessage({ id: 'overview.orders.expired' })}
                  </VisuallyHidden>
                </span>
              )}
            </div>
            <div className={styles.summary}>{o.summary}</div>
            <div className={styles.orderMeta}>
              <span className="tz-price">{formatMoney(o.totalPaise, 'dashboard')}</span>
              <StatusPill tone="pending">
                {o.fulfilment === 'pickup'
                  ? intl.formatMessage({ id: 'overview.orders.pickup' })
                  : intl.formatMessage({ id: 'overview.orders.delivery' })}
              </StatusPill>
            </div>
            {acting === null && action.error !== null && lastId === o.id && <ActionError error={action.error} />}
            <div className="tz-ocard-actions">
              <Button size="sm" onClick={() => reject(o.id)} aria-label={intl.formatMessage({ id: 'overview.orders.rejectA11y' }, { code: o.code })}>
                {intl.formatMessage({ id: 'overview.orders.reject' })}
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={() => {
                  setLastId(o.id);
                  void accept(o.id);
                }}
                disabled={acting !== null}
                aria-busy={acting === o.id || undefined}
                aria-label={intl.formatMessage({ id: 'overview.orders.acceptA11y' }, { code: o.code })}
              >
                {intl.formatMessage({ id: 'overview.orders.accept' })}
              </Button>
            </div>
          </article>
        );
      })}
    </section>
  );
}
