import type { Banner } from '@todayz/api-client/gen/model/banner';
import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Image } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { whenText } from '../stock/labels';
import { offersErrorKey, setBannerStatus, useBanners } from './queries';
import { bannerTone } from './summary';
import styles from './offers.module.css';

/** The shop's banners with their reach and moderation state. */
export function BannersPanel({ store, onNew }: { store: CurrentStore; onNew: () => void }) {
  const intl = useIntl();
  const list = useBanners(store, true);
  const all = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);

  if (list.isError) {
    return (
      <EmptyState
        icon={Image}
        title={intl.formatMessage({ id: 'offers.error.title' })}
        description={intl.formatMessage({ id: 'offers.error.body' })}
        action={<Button loading={list.isFetching} onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'offers.error.retry' })}</Button>}
      />
    );
  }
  if (list.isPending) {
    return (
      <div role="status" aria-busy="true" aria-label={intl.formatMessage({ id: 'offers.loading' })}>
        <Skeleton shape="block" height="160px" />
      </div>
    );
  }
  if (all.length === 0) {
    return (
      <EmptyState
        icon={Image}
        title={intl.formatMessage({ id: 'offers.banner.empty.title' })}
        description={intl.formatMessage({ id: 'offers.banner.empty.body' })}
        action={<Button variant="primary" onClick={onNew}>{intl.formatMessage({ id: 'offers.banner.new' })}</Button>}
      />
    );
  }
  return (
    <>
      <ul className={styles.rows} aria-label={intl.formatMessage({ id: 'offers.banner.list' })}>
        {all.map((b) => (
          <BannerRow key={b.id} store={store} banner={b} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className={styles.more}>
          <Button loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {intl.formatMessage({ id: 'offers.more' })}
          </Button>
        </div>
      )}
    </>
  );
}

function BannerRow({ store, banner }: { store: CurrentStore; banner: Banner }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const act = async (status: 'live' | 'paused' | 'ended') => {
    setBusy(true);
    setError(null);
    try {
      await setBannerStatus(qc, store, banner, status);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const removed = banner.status === 'removed' || banner.moderation === 'rejected';
  const final = banner.status === 'ended' || banner.status === 'removed';
  return (
    <li className={styles.row}>
      <div className={styles.rowHead}>
        <strong className={styles.grow}>{banner.headline}</strong>
        <StatusPill tone={bannerTone(banner.status)}>{intl.formatMessage({ id: `offers.banner.status.${banner.status}` })}</StatusPill>
      </div>
      {banner.status === 'pending_review' && <p className={styles.sub}>{intl.formatMessage({ id: 'offers.banner.checking' })}</p>}
      {removed && (
        <p className={styles.sub}>
          {banner.removeReason
            ? intl.formatMessage({ id: 'offers.banner.removedWhy' }, { reason: banner.removeReason })
            : intl.formatMessage({ id: 'offers.banner.removed' })}
        </p>
      )}
      <p className={styles.sub}>
        {intl.formatMessage({ id: 'offers.banner.reach' }, { views: banner.impressions, taps: banner.clicks, orders: banner.attributedOrders })} ·{' '}
        {banner.endsAt ? intl.formatMessage({ id: 'offers.row.until' }, { when: whenText(intl, banner.endsAt) }) : intl.formatMessage({ id: 'offers.row.noEnd' })}
      </p>
      {error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `offers.error.${offersErrorKey(error)}` })}
        </p>
      )}
      {!final && banner.status !== 'pending_review' && (
        <div className={styles.actions}>
          {banner.status === 'paused' ? (
            <Button loading={busy} onClick={() => void act('live')}>{intl.formatMessage({ id: 'offers.resume' })}</Button>
          ) : (
            <Button loading={busy} onClick={() => void act('paused')}>{intl.formatMessage({ id: 'offers.pause' })}</Button>
          )}
          <Button variant="soft-danger" loading={busy} onClick={() => void act('ended')}>{intl.formatMessage({ id: 'offers.end' })}</Button>
        </div>
      )}
    </li>
  );
}
