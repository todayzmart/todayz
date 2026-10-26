import { Banner, Button, Chip, EmptyState, Skeleton } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Inbox, MessageSquare, Store as StoreIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import type { StoreReview } from '@todayz/api-client/gen/model/storeReview';
import { LOW_STARS_MAX } from './limits';
import { canReviews } from './permissions';
import { useReviews } from './queries';
import { ReportSheet } from './ReportSheet';
import { ReviewCard } from './ReviewCard';
import { Summary } from './Summary';
import styles from './reviews.module.css';

type Filter = 'all' | 'needsReply' | 'withText' | 'low';

/** The API lists newest first with no filters, so these narrow the reviews loaded so far. */
const FILTERS: Record<Filter, (r: StoreReview) => boolean> = {
  all: () => true,
  needsReply: (r) => r.reply === null,
  withText: (r) => Boolean(r.text),
  low: (r) => r.stars <= LOW_STARS_MAX,
};

/** B-11 Reviews: the rating summary, the reviews, one editable reply each, and reporting. */
export function ReviewsPage() {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'reviews.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'reviews.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'reviews.error.title' })}
          description={intl.formatMessage({ id: 'reviews.error.body' })}
          action={<Button loading={current.retrying} onClick={current.retry}>{intl.formatMessage({ id: 'reviews.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'reviews.noStore.title' })}
          description={intl.formatMessage({ id: 'reviews.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/reviews' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'reviews.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' &&
        (canReviews(current.store.role) ? (
          <ReviewsDesk store={current.store} />
        ) : (
          <Banner tone="info">{intl.formatMessage({ id: 'reviews.askManager' })}</Banner>
        ))}
    </div>
  );
}

function ReviewsDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const [filter, setFilter] = useState<Filter>('all');
  const [reporting, setReporting] = useState<string | null>(null);
  const list = useReviews(store);
  const all = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const items = useMemo(() => all.filter(FILTERS[filter]), [all, filter]);
  const rating = list.data?.pages[0]?.rating;

  if (list.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'reviews.error.title' })}
        description={intl.formatMessage({ id: 'reviews.error.body' })}
        action={<Button loading={list.isFetching} onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'reviews.error.retry' })}</Button>}
      />
    );
  }
  if (list.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'reviews.loading' })}>
        <Skeleton height="10rem" />
      </div>
    );
  }
  const none = all.length === 0;

  return (
    <>
      <p className={styles.sub}>{store.name}</p>
      {rating && rating.count > 0 && <Summary rating={rating} />}
      {none ? (
        <EmptyState
          icon={MessageSquare}
          title={intl.formatMessage({ id: 'reviews.empty.title' })}
          description={intl.formatMessage({ id: 'reviews.empty.body' })}
        />
      ) : (
        <>
          <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'reviews.filter.label' })}>
            {(Object.keys(FILTERS) as Filter[]).map((f) => (
              <Chip key={f} pressed={filter === f} onPressedChange={() => setFilter(f)}>
                {intl.formatMessage({ id: `reviews.filter.${f}` })}
              </Chip>
            ))}
          </div>
          {items.length === 0 ? (
            <p className={styles.note}>{intl.formatMessage({ id: 'reviews.empty.filtered' })}</p>
          ) : (
            <ul className={styles.list} aria-label={intl.formatMessage({ id: 'reviews.list.label' })}>
              {items.map((r) => (
                <ReviewCard key={r.id} store={store} review={r} onReport={setReporting} />
              ))}
            </ul>
          )}
          {list.hasNextPage && (
            <div className={styles.more}>
              <Button loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
                {intl.formatMessage({ id: 'reviews.more' })}
              </Button>
            </div>
          )}
        </>
      )}
      {reporting && <ReportSheet store={store} reviewId={reporting} onClose={() => setReporting(null)} />}
    </>
  );
}
