// C-03 Home (D-249, D-269): where you are, the wallet, a sticky search with the mic and the pickup or
// delivery switch, filters, offers near you, the categories as pastel photo tiles, product rails from the
// nearest shops, each category's subcategories, the counts, the map and the shops near you as photo cards.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { getDiscoverSummary, listDiscoverStores } from '@todayz/api-client/gen/discover/discover';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useRouter } from '@tanstack/react-router';
import { Button, Chip, EmptyState, SearchField, Segmented, Skeleton, StoreCard } from '@todayz/ui';
import { BadgePercent, Bike, Clock, MapPin, ShoppingBag, Store as StoreIcon } from 'lucide-react';
import { lazy, Suspense, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { LimitBanner } from '../orders/LimitBanner';
import { Icon } from '../shell/Icon';
import { VoiceSearchSlot } from '../search/slots';
import { ChipSelect } from '../shell/ChipSelect';
import { CartBarLink } from '../store/CartBarLink';
import { useVoiceOutcome } from '../voice-search/session';
import { CategoryTiles, SubcategoryGrids } from './CategoryTiles';
import { useCoords, type Coords } from './coords';
import { useFeatureCardProps } from './feature';
import { Header } from './Header';
import { Offers } from './Offers';
import { radiusM, storeHref } from './present';
import { Rails } from './Rails';
import { useRailShops } from './railShops';
import { RouterLink } from './RouterLink';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from './radius';
import styles from './home.module.css';

// MapLibre stays out of the first load: the card is its own chunk.
const StoreMapCard = lazy(() => import('./StoreMapCard'));

// MapLibre's JS and WebGL setup cost the phone home its Lighthouse score even when they waited for the
// card to scroll near (W4-04): the card is a plain summary until a tap (D-251 B1). Once shown, it stays
// shown for the rest of the visit.
let mapShown = false;

/**
 * Which shops Home lists (D-269): every shop, or only those that take pickup or delivery orders. A
 * discovery filter only: the cart still asks pickup or delivery per order (C-09). Kept for the visit.
 */
type Mode = 'all' | 'pickup' | 'delivery';
let lastMode: Mode = 'all';

export function Home({ category }: { category?: string | undefined }) {
  const intl = useIntl();
  const router = useRouter();
  const coords = useCoords();
  const [q, setQ] = useState('');
  const [voiceNotice, setVoiceNotice] = useState<string | null>(null);
  const voice = useVoiceOutcome();
  const [mode, setModeState] = useState<Mode>(lastMode);
  const setMode = (m: Mode) => {
    lastMode = m;
    setModeState(m);
  };

  return (
    <div className={styles.page}>
      <Header coords={coords} />
      {/* Sticky under the header, as the big delivery apps do: search stays one tap away while the list scrolls. */}
      <div className={styles.searchBar}>
        <form
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            const text = q.trim();
            const source = text ? voice.searched(text) : undefined;
            const params = new URLSearchParams();
            if (text) params.set('q', text);
            if (source) params.set('source', source);
            const query = params.toString();
            router.history.push(query ? `/search?${query}` : '/search');
          }}
        >
          <SearchField
            label={intl.formatMessage({ id: 'home.search.label' })}
            placeholder={intl.formatMessage({ id: 'home.search.placeholder' })}
            value={q}
            onChange={setQ}
            enterKeyHint="search"
            trailing={
              <VoiceSearchSlot
                onNotice={setVoiceNotice}
                onTranscript={(heard) => {
                  voice.heard(heard);
                  setQ(heard.transcript);
                }}
              />
            }
          />
          {voiceNotice && (
            <p className={styles.voiceNotice} role="status">
              {voiceNotice}
            </p>
          )}
        </form>
        <Segmented
          className={styles.mode}
          label={intl.formatMessage({ id: 'home.mode.label' })}
          value={mode}
          onChange={setMode}
          options={[
            { value: 'all', label: intl.formatMessage({ id: 'home.mode.all' }), icon: StoreIcon },
            { value: 'pickup', label: intl.formatMessage({ id: 'home.filter.pickup' }), icon: ShoppingBag },
            { value: 'delivery', label: intl.formatMessage({ id: 'home.filter.delivery' }), icon: Bike },
          ]}
        />
      </div>
      <div className={styles.body}>
        <LimitBanner />
        {coords.status === 'none' && (
          <EmptyState
            icon={MapPin}
            title={intl.formatMessage({ id: 'home.noPlace.title' })}
            description={intl.formatMessage({ id: 'home.noPlace.body' })}
            action={
              <Link to="/location" className="tz-btn tz-btn-primary">
                {intl.formatMessage({ id: 'location.set' })}
              </Link>
            }
          />
        )}
        {coords.status === 'loading' && <Skeleton shape="block" height="96px" />}
        {coords.status === 'error' && (
          <EmptyState
            icon={MapPin}
            title={intl.formatMessage({ id: 'home.placeError.title' })}
            description={intl.formatMessage({ id: 'home.placeError.body' })}
            action={<Button onClick={coords.retry}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
          />
        )}
        {coords.status === 'ready' && <Nearby coords={coords.coords} category={category} mode={mode} />}
      </div>
      <CartBarLink />
    </div>
  );
}

type Filter = 'openNow' | 'hasOffer';
const FILTERS: readonly { key: Filter; label: string; icon: typeof Clock }[] = [
  { key: 'openNow', label: 'home.filter.openNow', icon: Clock },
  { key: 'hasOffer', label: 'home.filter.offers', icon: BadgePercent },
];

function Nearby({ coords, category, mode }: { coords: Coords; category: string | undefined; mode: Mode }) {
  const intl = useIntl();
  const router = useRouter();
  const [showMap, setShowMap] = useState(mapShown);
  const km = useRadiusKm();
  const cardProps = useFeatureCardProps();
  const [filters, setFilters] = useState<ReadonlySet<Filter>>(new Set());
  const { lat, lng } = coords;

  const summary = useQuery({
    queryKey: ['discover', 'summary', lat, lng, km],
    queryFn: () => getDiscoverSummary({ lat, lng, radius: radiusM(km) }),
  });
  const categories = useQuery({
    queryKey: ['catalogue', 'categories'],
    staleTime: 60 * 60_000,
    queryFn: () => listCategories(),
  });
  const stores = useInfiniteQuery({
    queryKey: ['discover', 'stores', lat, lng, km, category ?? null, mode, [...filters].sort()],
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      listDiscoverStores({
        lat,
        lng,
        radius: radiusM(km),
        ...(category ? { category } : {}),
        ...Object.fromEntries([...filters].map((f) => [f, true])),
        ...(mode === 'pickup' ? { pickup: true } : mode === 'delivery' ? { delivery: true } : {}),
        limit: 20,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });

  const items = useMemo(() => stores.data?.pages.flatMap((p) => p.items) ?? [], [stores.data]);
  const railShops = useRailShops(items);
  const names = useMemo(() => new Map(items.map((s) => [s.storeSlug, s.name] as const)), [items]);
  const nextKm = RADIUS_OPTIONS_KM.find((k) => k > km);
  const num = (n: number) => n.toLocaleString('en-IN');

  return (
    <>
      <div className={styles.filters} role="group" aria-label={intl.formatMessage({ id: 'home.filters.label' })}>
        <ChipSelect
          label={intl.formatMessage({ id: 'home.radius.label' })}
          value={km}
          options={RADIUS_OPTIONS_KM.map((k) => ({ value: k, label: intl.formatMessage({ id: 'home.radius.within' }, { km: String(k) }) }))}
          onChange={setRadiusKm}
        />
        {FILTERS.map((f) => (
          <Chip
            key={f.key}
            icon={f.icon}
            pressed={filters.has(f.key)}
            onPressedChange={(on) =>
              setFilters((prev) => {
                const next = new Set(prev);
                if (on) next.add(f.key);
                else next.delete(f.key);
                return next;
              })
            }
          >
            {intl.formatMessage({ id: f.label })}
          </Chip>
        ))}
      </div>

      <Offers coords={coords} km={km} />

      {categories.isPending && (
        <div className={styles.catGrid} aria-hidden="true">
          {[0, 1, 2, 3, 4, 5].map((k) => (
            <Skeleton key={k} shape="block" height="112px" />
          ))}
        </div>
      )}
      {categories.data && categories.data.items.length > 0 && (
        <section className={styles.section} aria-labelledby="home-cats">
          <h2 id="home-cats" className={styles.railTitle}>
            {intl.formatMessage({ id: 'home.categories.title' })}
          </h2>
          <CategoryTiles categories={categories.data.items} active={category} />
          {category && (
            <div className={styles.filterNote}>
              <span className="tz-tag">
                {intl.formatMessage({ id: 'home.category.active' }, { name: categories.data.items.find((c) => c.slug === category)?.name ?? category })}
              </span>
              <RouterLink href={`/c/${encodeURIComponent(category)}`} className="tz-btn tz-btn-ghost tz-btn-sm">
                {intl.formatMessage({ id: 'home.category.page' })}
              </RouterLink>
              <RouterLink href="/" className="tz-btn tz-btn-ghost tz-btn-sm">
                {intl.formatMessage({ id: 'home.category.clear' })}
              </RouterLink>
            </div>
          )}
        </section>
      )}

      <Rails shops={railShops} part="top" />

      {categories.data && !category && <SubcategoryGrids categories={categories.data.items} />}

      <Rails shops={railShops} part="bottom" />

      <dl className={styles.stats} aria-busy={summary.isPending}>
        <div className={styles.stat}>
          <dt className={styles.statLabel}>{intl.formatMessage({ id: 'home.stats.shops' })}</dt>
          <dd className={styles.statValue}>{summary.data ? num(summary.data.storesNearby) : <Skeleton width="2ch" />}</dd>
        </div>
        <div className={styles.stat}>
          <dt className={styles.statLabel}>{intl.formatMessage({ id: 'home.stats.open' })}</dt>
          <dd className={styles.statValue}>{summary.data ? num(summary.data.openNow) : <Skeleton width="2ch" />}</dd>
        </div>
        <div className={styles.stat}>
          <dt className={styles.statLabel}>{intl.formatMessage({ id: 'home.stats.products' })}</dt>
          <dd className={styles.statValue}>{summary.data ? num(summary.data.productsInRadius) : <Skeleton width="3ch" />}</dd>
        </div>
      </dl>

      <section className={styles.section} aria-label={intl.formatMessage({ id: 'home.map.label' })}>
        {showMap ? (
          <Suspense fallback={<Skeleton shape="block" height="220px" />}>
            <StoreMapCard
              coords={coords}
              km={km}
              summary={summary.data}
              names={names}
              interactive={false}
              onExpand={() => router.history.push('/map')}
              onSelectShop={(slug) => router.history.push(storeHref(slug))}
            />
          </Suspense>
        ) : (
          <button
            type="button"
            className={`tz-card ${styles.mapEntry}`}
            onClick={() => {
              mapShown = true;
              setShowMap(true);
            }}
          >
            <Icon icon={MapPin} />
            <span className={styles.basketText}>
              <span className="tz-card-title">{intl.formatMessage({ id: 'home.map.show' })}</span>
              <span className="tz-meta">
                {intl.formatMessage(
                  { id: 'home.map.mapLabel' },
                  { shops: num(summary.data?.storesNearby ?? 0), km: String(km), open: num(summary.data?.openNow ?? 0) },
                )}
              </span>
            </span>
          </button>
        )}
      </section>

      <section className={styles.section} aria-labelledby="home-shops">
        <h2 id="home-shops" className={styles.railTitle}>
          {intl.formatMessage({ id: 'home.shops.title' })}
        </h2>
        <div aria-busy={stores.isPending} className={styles.list}>
          {stores.isPending && (
            <>
              <Skeleton shape="block" height="240px" />
              <Skeleton shape="block" height="240px" />
            </>
          )}
          {items.map((s) => (
            <StoreCard key={s.storeSlug} {...cardProps(s)} />
          ))}
        </div>
        {stores.isError && (
          <EmptyState
            title={intl.formatMessage({ id: 'home.shops.error' })}
            action={<Button onClick={() => void stores.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
          />
        )}
        {stores.isSuccess && items.length === 0 && filters.size > 0 && (
          <EmptyState
            icon={StoreIcon}
            title={intl.formatMessage({ id: 'home.empty.filtered.title' })}
            description={intl.formatMessage({ id: 'home.empty.filtered.body' })}
            action={<Button onClick={() => setFilters(new Set())}>{intl.formatMessage({ id: 'home.filters.clear' })}</Button>}
          />
        )}
        {stores.isSuccess && items.length === 0 && filters.size === 0 && (
          <EmptyState
            icon={StoreIcon}
            title={intl.formatMessage({ id: 'home.empty.title' })}
            description={intl.formatMessage({ id: 'home.empty.body' })}
            action={
              nextKm !== undefined ? (
                <Button onClick={() => setRadiusKm(nextKm)}>
                  {intl.formatMessage({ id: 'home.empty.widen' }, { km: String(nextKm) })}
                </Button>
              ) : undefined
            }
          />
        )}
        {stores.hasNextPage && (
          <Button variant="soft" block loading={stores.isFetchingNextPage} onClick={() => void stores.fetchNextPage()}>
            {intl.formatMessage({ id: 'home.shops.more' })}
          </Button>
        )}
      </section>
    </>
  );
}
