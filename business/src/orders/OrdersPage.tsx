import { Banner, Button, EmptyState, Skeleton, Tabs, type TabItem } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { BellRing, Inbox, Store as StoreIcon, Volume2, VolumeX } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useAlertingOrders } from './alerts';
import { NewOrderCard, OrderRow } from './OrderCards';
import { TABS, useOrderList, useOrdersStream, type OrderTab } from './queries';
import { useSoundAlert } from './sound';
import { useCurrentStore, type CurrentStore } from './store';
import styles from './orders.module.css';

type TabValue = OrderTab | 'asks';

/** B-04 Orders: the business app's order desk. */
export function OrdersPage() {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
          <Skeleton height="6rem" />
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
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'orders.noStore.title' })}
          description={intl.formatMessage({ id: 'orders.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/orders' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'orders.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' && <OrdersDesk store={current.store} />}
    </div>
  );
}

function OrdersDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const [tab, setTab] = useState<TabValue>('new');
  const stream = useOrdersStream(store);
  const newList = useOrderList(store, 'new');
  const active = useOrderList(store, tab === 'asks' ? 'new' : tab);
  const alerting = useAlertingOrders(store.sid, newList.data?.items);
  const sound = useSoundAlert(alerting.length > 0);
  const newItems = newList.data?.items.length ?? 0;

  const tabs: TabItem<TabValue>[] = [
    { value: 'new', label: intl.formatMessage({ id: 'orders.tab.new' }), count: newItems },
    ...TABS.filter((t) => t !== 'new').map((t) => ({ value: t, label: intl.formatMessage({ id: `orders.tab.${t}` }) })),
    // TODO(W3-16): the Asks tab mounts B-20, the asks inbox. Empty slot until then.
    { value: 'asks', label: intl.formatMessage({ id: 'orders.tab.asks' }) },
  ];

  return (
    <>
      {stream === 'polling' && <Banner tone="warning" live="status">{intl.formatMessage({ id: 'orders.live.polling' })}</Banner>}
      {!sound.unlocked && !sound.muted && (
        <Banner
          icon={BellRing}
          title={intl.formatMessage({ id: 'orders.sound.title' })}
          action={
            <Button size="sm" variant="primary" onClick={sound.allow}>
              {intl.formatMessage({ id: 'orders.sound.allow' })}
            </Button>
          }
        >
          {intl.formatMessage({ id: 'orders.sound.body' })}
        </Banner>
      )}
      {sound.unlocked || sound.muted ? (
        <div className={styles.soundRow}>
          {sound.muted ? <VolumeX className="tz-ico tz-ico-sm" aria-hidden="true" /> : <Volume2 className="tz-ico tz-ico-sm" aria-hidden="true" />}
          <span>{intl.formatMessage({ id: sound.muted ? 'orders.sound.muted' : 'orders.sound.on' })}</span>
          <Button size="sm" variant="ghost" onClick={() => sound.setMuted(!sound.muted)}>
            {intl.formatMessage({ id: sound.muted ? 'orders.sound.unmute' : 'orders.sound.off' })}
          </Button>
        </div>
      ) : null}
      <Tabs label={intl.formatMessage({ id: 'orders.tabs.label' })} tabs={tabs} value={tab} onChange={setTab} />
      {tab === 'asks' ? (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'orders.asks.empty.title' })}
          description={intl.formatMessage({ id: 'orders.asks.empty.body' })}
        />
      ) : (
        <TabBody store={store} tab={tab} query={active} />
      )}
    </>
  );
}

function TabBody({ store, tab, query }: { store: CurrentStore; tab: OrderTab; query: ReturnType<typeof useOrderList> }) {
  const intl = useIntl();
  if (query.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'orders.error.title' })}
        description={intl.formatMessage({ id: 'orders.error.body' })}
        action={<Button onClick={() => void query.refetch()}>{intl.formatMessage({ id: 'orders.error.retry' })}</Button>}
      />
    );
  }
  if (!query.data) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'orders.loading' })}>
        <Skeleton height="6rem" />
      </div>
    );
  }
  const { items, serverNow } = query.data;
  if (items.length === 0) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: `orders.empty.${tab}.title` })}
        description={intl.formatMessage({ id: `orders.empty.${tab}.body` })}
      />
    );
  }
  const offsetMs = Date.parse(serverNow) - query.dataUpdatedAt;
  return (
    <ul className={styles.list} aria-label={intl.formatMessage({ id: `orders.tab.${tab}` })}>
      {items.map((o) => (
        <li key={o.id}>{tab === 'new' ? <NewOrderCard store={store} order={o} offsetMs={offsetMs} /> : <OrderRow order={o} />}</li>
      ))}
    </ul>
  );
}
