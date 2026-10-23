import { formatMoney } from '@todayz/i18n';
import { Button, StatusPill, useToast, VisuallyHidden } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Clock } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { mmss } from './format';
import styles from './overview.module.css';
import type { NewOrder } from './types';

/** Seconds since this card mounted, ticking once a second. */
function useElapsedSeconds(): number {
  const [start] = useState(() => Date.now());
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 1000);
    return () => clearInterval(t);
  }, [start]);
  return elapsed;
}

/**
 * New orders with the time left to accept. Sample data: Accept and Reject change nothing; they say
 * so in a toast that offers the Orders section (B-04).
 */
export function NewOrders({ orders }: { orders: readonly NewOrder[] }) {
  const intl = useIntl();
  const toast = useToast();
  const navigate = useNavigate();
  const elapsed = useElapsedSeconds();
  const sampleAction = () =>
    toast.show({
      message: intl.formatMessage({ id: 'overview.orders.sampleToast' }),
      action: {
        label: intl.formatMessage({ id: 'overview.orders.open' }),
        onClick: () => void navigate({ to: '/$section', params: { section: 'orders' } }),
      },
    });
  return (
    <section className={`tz-card ${styles.orders}`} aria-labelledby="ov-orders-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="ov-orders-title">
          {intl.formatMessage({ id: 'overview.orders.title' })}
        </h2>
        <StatusPill tone="pending">{intl.formatMessage({ id: 'overview.orders.waiting' }, { count: orders.length })}</StatusPill>
      </div>
      {orders.map((o) => {
        const left = Math.max(0, o.secondsToAccept - elapsed);
        const time = mmss(left);
        return (
          <article key={o.id} className="tz-ocard" aria-label={o.code}>
            <div className="tz-ocard-top">
              <span>{intl.formatMessage({ id: 'overview.orders.head' }, { code: o.code, customer: o.customer })}</span>
              <span className="tz-timer" data-testid="countdown">
                <Clock className="tz-ico tz-ico-sm" aria-hidden="true" />
                <span aria-hidden="true">{time}</span>
                <VisuallyHidden>
                  {left > 0
                    ? intl.formatMessage({ id: 'overview.orders.timeLeft' }, { time })
                    : intl.formatMessage({ id: 'overview.orders.expired' })}
                </VisuallyHidden>
              </span>
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
            <div className="tz-ocard-actions">
              <Button size="sm" onClick={sampleAction} aria-label={intl.formatMessage({ id: 'overview.orders.rejectA11y' }, { code: o.code })}>
                {intl.formatMessage({ id: 'overview.orders.reject' })}
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={sampleAction}
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
