import type { OrderSummary } from '@todayz/api-client/gen/model/orderSummary';
import { listMyOrders } from '@todayz/api-client/gen/orders/orders';
import { SessionPending, useSessionState } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Button, EmptyState, Skeleton, StatusPill, Tabs, orderStatusTone } from '@todayz/ui';
import { ChevronRight, MessageCircleQuestion, ReceiptIndianRupee } from 'lucide-react';
import { Icon } from '../shell/Icon';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import styles from '../cart/tx.module.css';
import { PathLink } from './ShopLink';
import { useMyOrderStream } from './stream';

type Tab = 'online' | 'store';

/** C-13 Orders: Online tab (active first, then history). The In store tab is a placeholder. */
export function OrdersPage() {
  const intl = useIntl();
  const { status } = useSessionState();
  const [tab, setTab] = useState<Tab>('online');

  if (status === 'signedOut') {
    return (
      <div className={styles.page}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.title' })}</h1>
        <EmptyState
          icon={ReceiptIndianRupee}
          title={intl.formatMessage({ id: 'orders.signedOut.title' })}
          description={intl.formatMessage({ id: 'orders.signedOut.body' })}
          action={
            <Link to="/sign-in" search={{ next: '/orders' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'orders.signedOut.cta' })}
            </Link>
          }
        />
      </div>
    );
  }
  if (status === 'unknown') {
    return (
      <SessionPending
        className={styles.page}
        loadingLabel={intl.formatMessage({ id: 'orders.loading' })}
        offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
        offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
        retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
      />
    );
  }
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.title' })}</h1>
      {/* C-26: the asks list is also reached from Orders (W3-16). */}
      <PathLink to="/asks" className={styles.asksRow}>
        <Icon icon={MessageCircleQuestion} />
        <span>{intl.formatMessage({ id: 'asks.ordersLink' })}</span>
        <Icon icon={ChevronRight} />
      </PathLink>
      <Tabs
        label={intl.formatMessage({ id: 'orders.tabs.label' })}
        value={tab}
        onChange={setTab}
        tabs={[
          { value: 'online', label: intl.formatMessage({ id: 'orders.tabs.online' }), panel: <Online /> },
          {
            value: 'store',
            label: intl.formatMessage({ id: 'orders.tabs.store' }),
            // TODO(lead): the In store tab (POS bills linked to the customer) is not in this slice.
            panel: (
              <EmptyState
                icon={ReceiptIndianRupee}
                title={intl.formatMessage({ id: 'orders.store.title' })}
                description={intl.formatMessage({ id: 'orders.store.body' })}
              />
            ),
          },
        ]}
      />
    </div>
  );
}

function Online() {
  const intl = useIntl();
  useMyOrderStream();
  const active = useQuery({ queryKey: ['orders', 'active'], queryFn: () => listMyOrders({ status: 'active' }) });
  const past = useQuery({ queryKey: ['orders', 'past'], queryFn: () => listMyOrders({ status: 'past' }) });

  if (active.isPending || past.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
        <Skeleton height="6rem" />
      </div>
    );
  }
  if (active.isError || past.isError) {
    return (
      <div role="alert" className={styles.stack}>
        <p>{intl.formatMessage({ id: 'orders.loadError' })}</p>
        <Button
          onClick={() => {
            void active.refetch();
            void past.refetch();
          }}
        >
          {intl.formatMessage({ id: 'orders.retry' })}
        </Button>
      </div>
    );
  }
  const now = active.data.items;
  const before = past.data.items;
  if (now.length === 0 && before.length === 0) {
    return (
      <EmptyState
        icon={ReceiptIndianRupee}
        title={intl.formatMessage({ id: 'orders.empty.title' })}
        description={intl.formatMessage({ id: 'orders.empty.body' })}
        action={
          <PathLink to="/" className="tz-btn tz-btn-primary">
            {intl.formatMessage({ id: 'orders.empty.cta' })}
          </PathLink>
        }
      />
    );
  }
  return (
    <div className={styles.stack}>
      {now.length > 0 && <Group title={intl.formatMessage({ id: 'orders.group.active' })} orders={now} />}
      {before.length > 0 && <Group title={intl.formatMessage({ id: 'orders.group.past' })} orders={before} />}
    </div>
  );
}

function Group({ title, orders }: { title: string; orders: OrderSummary[] }) {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-label={title}>
      <h2 className={styles.sectionTitle}>{title}</h2>
      {orders.map((o) => (
        <Link key={o.id} to="/orders/$orderId" params={{ orderId: o.id }} className={styles.orderRow}>
          <span className={styles.orderHead}>
            <span className={styles.name}>{o.store.name}</span>
            <StatusPill tone={orderStatusTone(o.status)}>{intl.formatMessage({ id: `orders.status.${o.status}` })}</StatusPill>
          </span>
          <span className={styles.meta}>
            {intl.formatMessage({ id: 'orders.row.meta' }, { number: o.number, count: o.itemCount })} · {formatMoney(o.totalPaise)}
          </span>
        </Link>
      ))}
    </section>
  );
}
