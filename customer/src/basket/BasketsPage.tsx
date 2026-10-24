// C-22 /baskets: the customer's saved baskets (name, line count, "Search again"), "New basket", and delete
// with an undo. Signed out, one unsaved basket lives on this device with "Sign in to save it".
import { deleteBasket } from '@todayz/api-client/gen/baskets/baskets';
import type { BasketSummary } from '@todayz/api-client/gen/model/basketSummary';
import { SessionPending, useSessionState } from '@todayz/auth';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button, EmptyState, Skeleton, useToast } from '@todayz/ui';
import { Plus, Search, ShoppingBasket, Trash2 } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { useFmt } from '../home/format';
import { RouterLink } from '../home/RouterLink';
import { Icon } from '../shell/Icon';
import { basketsKey, isLimit, loadBaskets, saveNew, stored } from './api';
import { useDeviceBasket } from './device';
import { SaveDeviceOffer } from './SaveDeviceOffer';
import styles from './basket.module.css';

/** How long "Undo" is offered before the basket is really deleted. */
const UNDO_MS = 8000;

export function BasketsPage() {
  const intl = useIntl();
  const { status } = useSessionState();
  return (
    <div className={styles.page}>
      <header className={`tz-hero-wash ${styles.head}`}>
        <h1 className={styles.title}>{intl.formatMessage({ id: 'basket.list.title' })}</h1>
        <p className={styles.sub}>{intl.formatMessage({ id: 'basket.list.sub' })}</p>
      </header>
      <div className={styles.body}>
        {status === 'signedOut' ? (
          <DeviceList />
        ) : status === 'unknown' ? (
          <SessionPending
            loadingLabel={intl.formatMessage({ id: 'basket.loading' })}
            offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
            offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
            retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
          />
        ) : (
          <SavedList />
        )}
      </div>
    </div>
  );
}

function DeviceList() {
  const intl = useIntl();
  const basket = useDeviceBasket();
  return (
    <>
      <Banner
        tone="info"
        action={
          <RouterLink href="/sign-in?next=%2Fbaskets" className="tz-btn tz-btn-soft tz-btn-sm">
            {intl.formatMessage({ id: 'basket.signIn' })}
          </RouterLink>
        }
      >
        {intl.formatMessage({ id: 'basket.device.note' })}
      </Banner>
      {basket.lines.length > 0 ? (
        <ul className={styles.list}>
          <li className={`tz-card ${styles.row}`}>
            <div className={styles.rowText}>
              <span className="tz-card-title">{basket.name || intl.formatMessage({ id: 'basket.device.title' })}</span>
              <span className="tz-meta">{intl.formatMessage({ id: 'basket.list.count' }, { n: basket.lines.length })}</span>
            </div>
            <RouterLink href="/baskets/device" className="tz-btn tz-btn-soft tz-btn-sm">
              <Icon icon={Search} />
              {intl.formatMessage({ id: 'basket.list.open' })}
            </RouterLink>
          </li>
        </ul>
      ) : (
        <EmptyState
          icon={ShoppingBasket}
          title={intl.formatMessage({ id: 'basket.list.empty.title' })}
          description={intl.formatMessage({ id: 'basket.list.empty.body' })}
          action={
            <RouterLink href="/baskets/device" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'basket.list.new' })}
            </RouterLink>
          }
        />
      )}
    </>
  );
}

function SavedList() {
  const intl = useIntl();
  const qc = useQueryClient();
  const router = useRouter();
  const toast = useToast();
  const { ago } = useFmt();
  const list = useQuery({ queryKey: basketsKey, queryFn: loadBaskets });
  const [limit, setLimit] = useState(false);
  // Deleted on screen, waiting out the undo window.
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const timers = useRef(new Map<string, number>());

  const remove = useMutation({
    mutationFn: (id: string) => deleteBasket(id),
    onSettled: () => void qc.invalidateQueries({ queryKey: basketsKey, exact: true }),
  });
  const { mutate: removeNow } = remove;

  // Leaving the page does not cancel a delete the customer asked for.
  useEffect(() => {
    const t = timers.current;
    return () => {
      for (const [id, timer] of t) {
        window.clearTimeout(timer);
        removeNow(id);
      }
      t.clear();
    };
  }, [removeNow]);

  const create = useMutation({
    mutationFn: () => saveNew(intl.formatMessage({ id: 'basket.defaultName' }), []),
    onSuccess: (b) => {
      stored(qc, b);
      router.history.push(`/baskets/${b.basketId}`);
    },
    onError: (err) => {
      if (isLimit(err)) setLimit(true);
      else toast.show({ message: intl.formatMessage({ id: 'basket.saveError' }) });
    },
  });

  const del = (b: BasketSummary) => {
    setHidden((h) => new Set(h).add(b.basketId));
    const undo = () => {
      const t = timers.current.get(b.basketId);
      if (t !== undefined) window.clearTimeout(t);
      timers.current.delete(b.basketId);
      setHidden((h) => {
        const next = new Set(h);
        next.delete(b.basketId);
        return next;
      });
    };
    timers.current.set(
      b.basketId,
      window.setTimeout(() => {
        timers.current.delete(b.basketId);
        removeNow(b.basketId);
      }, UNDO_MS),
    );
    setLimit(false);
    toast.show({
      message: intl.formatMessage({ id: 'basket.deleted' }, { name: b.name }),
      action: { label: intl.formatMessage({ id: 'basket.undo' }), onClick: undo },
      duration: UNDO_MS,
    });
  };

  const items = (list.data?.baskets ?? []).filter((b) => !hidden.has(b.basketId));
  const now = new Date().toISOString();

  return (
    <>
      <SaveDeviceOffer />
      {limit && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'basket.limit' })}
        </Banner>
      )}
      <Button variant="primary" icon={Plus} block loading={create.isPending} onClick={() => create.mutate()}>
        {intl.formatMessage({ id: 'basket.list.new' })}
      </Button>
      {list.isPending && <Skeleton shape="block" height="72px" />}
      {list.isError && (
        <EmptyState
          title={intl.formatMessage({ id: 'basket.error' })}
          action={<Button onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
        />
      )}
      {list.isSuccess && items.length === 0 && (
        <EmptyState
          icon={ShoppingBasket}
          title={intl.formatMessage({ id: 'basket.list.empty.title' })}
          description={intl.formatMessage({ id: 'basket.list.empty.body' })}
        />
      )}
      {items.length > 0 && (
        <ul className={styles.list}>
          {items.map((b) => (
            <li key={b.basketId} className={`tz-card ${styles.row}`}>
              <div className={styles.rowText}>
                <RouterLink href={`/baskets/${b.basketId}`} className={`tz-card-title ${styles.storeName}`}>
                  {b.name}
                </RouterLink>
                <span className="tz-meta">
                  {intl.formatMessage({ id: 'basket.list.count' }, { n: b.lineCount })} ·{' '}
                  {intl.formatMessage({ id: 'basket.list.updated' }, { time: ago(b.updatedAt, now) })}
                </span>
              </div>
              <div className={styles.rowActions}>
                <RouterLink href={`/baskets/${b.basketId}`} className="tz-btn tz-btn-soft tz-btn-sm">
                  <Icon icon={Search} />
                  {intl.formatMessage({ id: 'basket.list.searchAgain' })}
                </RouterLink>
                <Button
                  variant="ghost"
                  size="sm"
                  icon={Trash2}
                  aria-label={intl.formatMessage({ id: 'basket.list.delete' }, { name: b.name })}
                  onClick={() => del(b)}
                />
              </div>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
