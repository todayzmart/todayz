// C-07 Shop page: header (name, distance, open state with today's hours, rating, directions), in-shop
// search, category tabs, and the items with stock, freshness and Add to cart.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { getDiscoverStore, listDiscoverStoreItems } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverHours } from '@todayz/api-client/gen/model/discoverHours';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, EmptyState, SearchField, Skeleton, Tabs } from '@todayz/ui';
import { formatDistance } from '@todayz/i18n';
import { MapPin, Star, Store as StoreIcon } from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { useCoords } from '../home/coords';
import { useFmt, usePackLine } from '../home/format';
import { asTint, directionsUrl, initials } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { AskNearbySlot } from '../search/slots';
import { Icon } from '../shell/Icon';
import { CartBarLink } from './CartBarLink';
import { ItemCard } from './ItemCard';
import { rangesLabel, todayRanges } from './hours';
import { seedItems } from './itemCache';
import { rememberRef } from './ref';
import { useCartActions } from './useCartActions';
import styles from './store.module.css';

export interface StoreState {
  q?: string | undefined;
  cat?: string | undefined;
  r?: string | undefined;
}

export function StorePage({ slug, state, onChange }: { slug: string; state: StoreState; onChange: (next: StoreState) => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const { money } = useFmt();
  const packLine = usePackLine();
  const coordsState = useCoords();
  const coords = coordsState.status === 'ready' ? coordsState.coords : null;
  const { qtyOf, change, sheet } = useCartActions();

  // The QR deep link: keep the referral code on this device (01 §3).
  useEffect(() => rememberRef(state.r), [state.r]);

  const shop = useQuery({
    queryKey: ['discover', 'store', slug, coords?.lat ?? null, coords?.lng ?? null],
    retry: false,
    queryFn: () => getDiscoverStore(slug, coords ? { lat: coords.lat, lng: coords.lng } : undefined),
  });
  const categories = useQuery({
    queryKey: ['catalogue', 'categories'],
    staleTime: 60 * 60_000,
    queryFn: () => listCategories(),
  });
  const items = useInfiniteQuery({
    queryKey: ['discover', 'store-items', slug, state.q ?? '', state.cat ?? ''],
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      listDiscoverStoreItems(slug, {
        ...(state.q ? { q: state.q } : {}),
        ...(state.cat ? { category: state.cat } : {}),
        limit: 30,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });

  const pages = useMemo(() => items.data?.pages ?? [], [items.data]);
  useEffect(() => {
    for (const p of pages) seedItems(qc, slug, p.items, p.serverNow);
  }, [pages, qc, slug]);

  if (shop.isError) {
    return (
      <div className={styles.body}>
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'store.gone.title' })}
          description={intl.formatMessage({ id: 'store.gone.body' })}
          action={
            <RouterLink href="/search" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'store.gone.action' })}
            </RouterLink>
          }
        />
      </div>
    );
  }

  const s = shop.data;
  const all = pages.flatMap((p) => p.items.map((item) => ({ item, serverNow: p.serverNow })));
  const bookable = s?.bookable ?? true;

  const list = (
    <div aria-busy={items.isPending}>
      {items.isPending && (
        <div className={styles.items}>
          <Skeleton shape="block" height="190px" />
          <Skeleton shape="block" height="190px" />
        </div>
      )}
      {items.isError && (
        <EmptyState
          title={intl.formatMessage({ id: 'store.items.error' })}
          action={<Button onClick={() => void items.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
        />
      )}
      {items.isSuccess && all.length === 0 && (
        <EmptyState
          title={intl.formatMessage({ id: state.q || state.cat ? 'store.items.noMatch' : 'store.items.none' })}
          description={intl.formatMessage({ id: state.q || state.cat ? 'store.items.noMatchBody' : 'store.items.noneBody' })}
        />
      )}
      <div className={styles.items}>
        {all.map(({ item, serverNow }) => (
          <ItemCard
            key={item.storeItemId}
            shopSlug={slug}
            item={item}
            serverNow={serverNow}
            bookable={bookable}
            qty={qtyOf(slug, item.storeItemId)}
            onChange={(qty) => {
              if (s) change({ id: s.storeId, slug, name: s.name }, item, packLine(item), qty);
            }}
            directionsHref={s ? directionsUrl(s.lat, s.lng) : undefined}
          />
        ))}
      </div>
      {items.hasNextPage && (
        <Button variant="soft" block loading={items.isFetchingNextPage} onClick={() => void items.fetchNextPage()}>
          {intl.formatMessage({ id: 'store.items.more' })}
        </Button>
      )}
    </div>
  );

  const cats = categories.data?.items ?? [];
  const tabValue = state.cat ?? 'all';
  const tabs = [
    { value: 'all', label: intl.formatMessage({ id: 'search.category.all' }), panel: tabValue === 'all' ? list : undefined },
    ...cats.map((c) => ({ value: c.slug, label: c.name, panel: tabValue === c.slug ? list : undefined })),
  ];

  return (
    <div className={styles.page}>
      <header className={`tz-hero-wash ${styles.head}`}>
        {s ? (
          <>
            <div className={styles.titleRow}>
              <div className="tz-avatar" data-tint={asTint(s.categoryTint)} aria-hidden="true">
                {initials(s.name)}
              </div>
              <div className="tz-ui-minw0">
                <h1 className={styles.name}>{s.name}</h1>
                <div className="tz-meta">
                  {s.distanceM !== null && <span>{formatDistance(s.distanceM)}</span>}
                  {s.locality && <span>{s.locality}</span>}
                  {s.rating !== null && (
                    <span className={styles.rating}>
                      <Icon icon={Star} />
                      {intl.formatMessage({ id: 'store.rating' }, { rating: s.rating.toFixed(1), count: String(s.ratingCount) })}
                    </span>
                  )}
                </div>
              </div>
            </div>
            <div className="tz-meta tz-ui-meta-tags">
              <span className={s.open ? 'tz-tag tz-tag-open' : 'tz-tag'}>
                {intl.formatMessage({ id: s.open ? 'store.open' : 'store.closed' })}
              </span>
              <span className="tz-tag">{hoursText(intl, s.hours, s.serverNow)}</span>
              {s.pickup && <span className="tz-tag">{intl.formatMessage({ id: 'store.pickup' })}</span>}
              {s.delivery && <span className="tz-tag">{intl.formatMessage({ id: 'store.delivery' })}</span>}
              {s.cashbackPct > 0 && (
                <span className="tz-tag tz-tag-offer">{intl.formatMessage({ id: 'store.cashback' }, { pct: String(s.cashbackPct) })}</span>
              )}
            </div>
            {s.delivery && (
              <p className={styles.note}>
                {intl.formatMessage(
                  { id: 'store.deliveryNote' },
                  {
                    fee: s.deliveryFeePaise === 0 ? intl.formatMessage({ id: 'store.deliveryFree' }) : money(s.deliveryFeePaise),
                    min: money(s.deliveryMinOrderPaise),
                  },
                )}
              </p>
            )}
            {/* TODO(lead): no phone in the shop page contract, so there is no Call button yet. */}
            <div className={styles.actions}>
              <a className="tz-btn tz-btn-soft tz-btn-sm" href={directionsUrl(s.lat, s.lng)} target="_blank" rel="noopener noreferrer">
                <Icon icon={MapPin} />
                {intl.formatMessage({ id: 'store.directions' })}
              </a>
            </div>
          </>
        ) : (
          <>
            <Skeleton width="60%" height="28px" />
            <Skeleton width="40%" />
          </>
        )}
      </header>

      <div className={styles.body}>
        {!bookable && <Banner tone="info">{intl.formatMessage({ id: 'store.inStoreOnly.banner' })}</Banner>}
        <form role="search" onSubmit={(e) => e.preventDefault()}>
          <SearchField
            label={intl.formatMessage({ id: 'store.search.label' })}
            placeholder={intl.formatMessage({ id: 'store.search.placeholder' })}
            clearLabel={intl.formatMessage({ id: 'search.field.clear' })}
            defaultValue={state.q ?? ''}
            onSearch={(v) => onChange({ ...state, q: v.trim() || undefined })}
            enterKeyHint="search"
          />
        </form>
        <Tabs
          label={intl.formatMessage({ id: 'search.category.label' })}
          tabs={tabs}
          value={tabValue}
          onChange={(v) => onChange({ ...state, cat: v === 'all' ? undefined : v })}
        />
        <AskNearbySlot query={state.q ?? ''} shopSlug={slug} />
      </div>
      {sheet}
      <CartBarLink />
    </div>
  );
}

function hoursText(intl: IntlShape, hours: DiscoverHours, serverNow: string): string {
  const ranges = todayRanges(hours, serverNow);
  if (ranges.length === 0) return intl.formatMessage({ id: 'store.hours.closedToday' });
  const range = (from: string, to: string) => intl.formatMessage({ id: 'store.hours.range' }, { from, to });
  return intl.formatMessage({ id: 'store.hours.today' }, { hours: rangesLabel(ranges, range) });
}
