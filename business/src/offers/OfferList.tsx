import type { Offer } from '@todayz/api-client/gen/model/offer';
import type { OfferStatus } from '@todayz/api-client/gen/model/offerStatus';
import { Button, Chip, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Tag } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { whenText } from '../stock/labels';
import { offersErrorKey, setOfferStatus, useOffers } from './queries';
import { offerHeadline, offerScopeText, offerTone } from './summary';
import styles from './offers.module.css';

type Filter = 'all' | OfferStatus;
const FILTERS: readonly Filter[] = ['all', 'live', 'scheduled', 'paused', 'ended'];

/** The shop's offers with status, redemptions and pause, resume and end. Terms never change after creation. */
export function OfferList({ store, onNew }: { store: CurrentStore; onNew: () => void }) {
  const intl = useIntl();
  const [filter, setFilter] = useState<Filter>('all');
  const list = useOffers(store);
  const all = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const shown = filter === 'all' ? all : all.filter((o) => o.status === filter);

  if (list.isError) {
    return (
      <EmptyState
        icon={Tag}
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
        icon={Tag}
        title={intl.formatMessage({ id: 'offers.empty.title' })}
        description={intl.formatMessage({ id: 'offers.empty.body' })}
        action={<Button variant="primary" onClick={onNew}>{intl.formatMessage({ id: 'offers.new' })}</Button>}
      />
    );
  }
  return (
    <>
      <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'offers.filter.label' })}>
        {FILTERS.map((f) => (
          <Chip key={f} pressed={filter === f} onPressedChange={() => setFilter(f)}>
            {intl.formatMessage({ id: `offers.filter.${f}` })}
          </Chip>
        ))}
      </div>
      {shown.length === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'offers.empty.filtered' })}</p>
      ) : (
        <ul className={styles.rows} aria-label={intl.formatMessage({ id: 'offers.list.label' })}>
          {shown.map((o) => (
            <OfferRow key={o.id} store={store} offer={o} />
          ))}
        </ul>
      )}
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

function OfferRow({ store, offer }: { store: CurrentStore; offer: Offer }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [confirmEnd, setConfirmEnd] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const act = async (status: 'live' | 'paused' | 'ended') => {
    setBusy(true);
    setError(null);
    try {
      await setOfferStatus(qc, store, offer, status);
      setConfirmEnd(false);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const open = offer.status !== 'ended';
  return (
    <li className={styles.row}>
      <div className={styles.rowHead}>
        <strong className={styles.grow}>{offer.title}</strong>
        <StatusPill tone={offerTone(offer.status)}>{intl.formatMessage({ id: `offers.status.${offer.status}` })}</StatusPill>
      </div>
      <p className={styles.sub}>
        {offerHeadline(intl, offer)} · {offerScopeText(intl, offer)}
      </p>
      <p className={styles.sub}>
        {intl.formatMessage({ id: 'offers.row.redemptions' }, { count: offer.redemptions, max: offer.maxRedemptions ?? 0 })} ·{' '}
        {offer.endsAt
          ? intl.formatMessage({ id: 'offers.row.until' }, { when: whenText(intl, offer.endsAt) })
          : intl.formatMessage({ id: 'offers.row.noEnd' })}
      </p>
      {error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `offers.error.${offersErrorKey(error)}` })}
        </p>
      )}
      {open && (
        <div className={styles.actions}>
          {confirmEnd ? (
            <>
              <span className={styles.grow}>{intl.formatMessage({ id: 'offers.end.confirm' })}</span>
              <Button onClick={() => setConfirmEnd(false)}>{intl.formatMessage({ id: 'offers.end.keep' })}</Button>
              <Button variant="danger" loading={busy} onClick={() => void act('ended')}>
                {intl.formatMessage({ id: 'offers.end.yes' })}
              </Button>
            </>
          ) : (
            <>
              {offer.status === 'paused' ? (
                <Button loading={busy} onClick={() => void act('live')}>{intl.formatMessage({ id: 'offers.resume' })}</Button>
              ) : (
                <Button loading={busy} onClick={() => void act('paused')}>{intl.formatMessage({ id: 'offers.pause' })}</Button>
              )}
              <Button variant="soft-danger" onClick={() => setConfirmEnd(true)}>{intl.formatMessage({ id: 'offers.end' })}</Button>
            </>
          )}
        </div>
      )}
    </li>
  );
}
