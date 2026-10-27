// C-07 Shop page (D-249 bright): the shop's photo as a cover (its category's stock photo without one), a
// white card over it (name, rating, distance, open state with today's hours, pickup or delivery, directions,
// cashback), a sticky in-shop search with category chips, and the items with stock, freshness and Add.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { getDiscoverStore, listDiscoverStoreItems } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverHours } from '@todayz/api-client/gen/model/discoverHours';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, ChipGroup, EmptyState, SearchField, Skeleton } from '@todayz/ui';
import { formatDistance } from '@todayz/i18n';
import { BadgePercent, Bike, Clock, MapPin, Navigation, ShoppingBag, Star, Store as StoreIcon, Wallet } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
import { useCoords } from '../home/coords';
import { useFmt, usePackLine } from '../home/format';
import { asTint, directionsUrl, imageUrl, initials } from '../home/present';
import { tintPhoto } from '../home/photos';
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
          {[0, 1, 2, 3, 4, 5].map((k) => (
            <div key={k} className={styles.skelTile}>
              <Skeleton shape="block" height="auto" />
              <Skeleton width="40%" />
              <Skeleton width="85%" />
            </div>
          ))}
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

  return (
    <div className={styles.page}>
      <header className={styles.head}>
        <div className={styles.cover} aria-hidden="true">
          {s && <img src={imageUrl(s.photoKey) ?? tintPhoto(asTint(s.categoryTint))} alt="" decoding="async" />}
        </div>
        <div className={styles.card}>
          {s ? (
            <>
              <div className={styles.titleRow}>
                <StoreAvatar name={s.name} tint={s.categoryTint} photoKey={s.photoKey} />
                <div className={styles.titleText}>
                  <h1 className={styles.name}>{s.name}</h1>
                  <div className={`tz-meta ${styles.meta}`}>
                    {s.distanceM !== null && (
                      <span>
                        <Icon icon={MapPin} />
                        {formatDistance(s.distanceM)}
                      </span>
                    )}
                    {s.categoryName && (
                      <span>
                        <Icon icon={StoreIcon} />
                        {s.categoryName}
                      </span>
                    )}
                    {s.locality && <span>{s.locality}</span>}
                  </div>
                </div>
                {s.rating !== null && (
                  <span
                    className={styles.rate}
                    role="img"
                    aria-label={intl.formatMessage({ id: 'home.card.rating' }, { rating: s.rating.toFixed(1), count: s.ratingCount })}
                  >
                    <Icon icon={Star} />
                    {s.rating.toFixed(1)}
                  </span>
                )}
              </div>

              <ul className={styles.facts}>
                <li>
                  <Icon icon={Clock} />
                  <span className={s.open ? styles.open : styles.closed}>{intl.formatMessage({ id: s.open ? 'store.open' : 'store.closed' })}</span>
                  <span>{hoursText(intl, s.hours, s.serverNow)}</span>
                </li>
                {(s.pickup || s.delivery) && (
                  <li>
                    <Icon icon={s.delivery ? Bike : ShoppingBag} />
                    <span>
                      {[s.pickup && intl.formatMessage({ id: 'store.pickup' }), s.delivery && intl.formatMessage({ id: 'store.delivery' })]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </li>
                )}
                {s.delivery && (
                  <li className={styles.note}>
                    <Icon icon={Wallet} />
                    <span>
                      {intl.formatMessage(
                        { id: 'store.deliveryNote' },
                        {
                          fee: s.deliveryFeePaise === 0 ? intl.formatMessage({ id: 'store.deliveryFree' }) : money(s.deliveryFeePaise),
                          min: money(s.deliveryMinOrderPaise),
                        },
                      )}
                    </span>
                  </li>
                )}
              </ul>

              {/* TODO(lead): no phone in the shop page contract, so there is no Call button yet. */}
              <div className={styles.actions}>
                <a className="tz-btn tz-btn-soft tz-btn-sm" href={directionsUrl(s.lat, s.lng)} target="_blank" rel="noopener noreferrer">
                  <Icon icon={Navigation} />
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
          {s && s.cashbackPct > 0 && (
            <p className={styles.offer}>
              <Icon icon={BadgePercent} />
              {intl.formatMessage({ id: 'home.card.footer.cashback' }, { pct: String(s.cashbackPct) })}
            </p>
          )}
        </div>
      </header>

      <div className={styles.band}>
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
        {cats.length > 0 && (
          <ChipGroup
            label={intl.formatMessage({ id: 'search.category.label' })}
            options={[{ value: '', label: intl.formatMessage({ id: 'search.category.all' }) }, ...cats.map((c) => ({ value: c.slug, label: c.name }))]}
            value={state.cat ?? ''}
            onChange={(v) => onChange({ ...state, cat: v || undefined })}
          />
        )}
      </div>

      <div className={styles.body}>
        {!bookable && <Banner tone="info">{intl.formatMessage({ id: 'store.inStoreOnly.banner' })}</Banner>}
        {list}
        <AskNearbySlot query={state.q ?? ''} shopSlug={slug} />
      </div>
      {sheet}
      <CartBarLink />
    </div>
  );
}

/** The shop's photo once an admin has accepted one (D-246), else its initials; the initials again if it fails to load. */
function StoreAvatar({ name, tint, photoKey }: { name: string; tint: string | null; photoKey: string | null }) {
  const src = imageUrl(photoKey);
  const [failed, setFailed] = useState<string | undefined>();
  const photo = src !== undefined && src !== failed ? src : undefined;
  return (
    <div className={photo ? 'tz-avatar tz-ui-avatar-photo' : 'tz-avatar'} data-tint={asTint(tint)} aria-hidden="true">
      {photo ? (
        <img className="tz-ui-thumb-img" src={photo} alt="" decoding="async" onError={() => setFailed(photo)} />
      ) : (
        initials(name)
      )}
    </div>
  );
}

function hoursText(intl: IntlShape, hours: DiscoverHours, serverNow: string): string {
  const ranges = todayRanges(hours, serverNow);
  if (ranges.length === 0) return intl.formatMessage({ id: 'store.hours.closedToday' });
  const range = (from: string, to: string) => intl.formatMessage({ id: 'store.hours.range' }, { from, to });
  return intl.formatMessage({ id: 'store.hours.today' }, { hours: rangesLabel(ranges, range) });
}
