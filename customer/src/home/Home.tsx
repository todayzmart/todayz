// C-03 Home: where you are, what is around you (counts from the discover summary), the radius, categories
// and the shops near you. The full-screen map (C-04) and banners (W2-07) are later.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { getDiscoverSummary, listDiscoverStores } from '@todayz/api-client/gen/discover/discover';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useRouter } from '@tanstack/react-router';
import { CategoryRow, EmptyState, RadiusChips, SearchField, Skeleton, StoreCard, Button } from '@todayz/ui';
import { MapPin, ShoppingBasket, Store as StoreIcon } from 'lucide-react';
import { lazy, Suspense, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { usePlace } from '../app/place';
import { LimitBanner } from '../orders/LimitBanner';
import { Icon } from '../shell/Icon';
import { CartBarLink } from '../store/CartBarLink';
import { categoryIcon } from './categories';
import { useCoords, type Coords } from './coords';
import { asTint, radiusM, storeHref, useStoreCardProps } from './present';
import { RouterLink } from './RouterLink';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from './radius';
import styles from './home.module.css';

// MapLibre stays out of the first load: the card is its own chunk.
const StoreMapCard = lazy(() => import('./StoreMapCard'));

// MapLibre's JS and WebGL setup cost the phone home its Lighthouse score even when they waited for the
// card to scroll near (W4-04): the card is a plain summary until a tap (D-251 B1). Once shown, it stays
// shown for the rest of the visit.
let mapShown = false;

export function Home({ category }: { category?: string | undefined }) {
  const intl = useIntl();
  const router = useRouter();
  const place = usePlace();
  const coords = useCoords();
  const [q, setQ] = useState('');

  const where =
    place?.kind === 'pin'
      ? intl.formatMessage({ id: 'location.current' }, { place: place.pin })
      : place?.kind === 'gps'
        ? intl.formatMessage({ id: 'location.current' }, { place: intl.formatMessage({ id: 'location.gps.label' }).toLowerCase() })
        : intl.formatMessage({ id: 'location.set' });

  return (
    <>
      <header className={`tz-hero-wash ${styles.hero}`}>
        <div className={styles.brandRow}>
          <span className="tz-side-brand">
            <img className={styles.mark} src="/icons/mark-96.png" alt="" width={28} height={28} />
            <span>
              Today<b>Z</b>
            </span>
          </span>
          <Link to="/location" className={`tz-btn tz-btn-soft tz-btn-sm ${styles.placeChip}`} title={where}>
            <Icon icon={MapPin} />
            <span className={styles.placeText}>{where}</span>
          </Link>
        </div>
        <h1 className={styles.heroTitle}>{intl.formatMessage({ id: 'shell.hero.title' })}</h1>
        <p className={styles.heroSub}>{intl.formatMessage({ id: 'shell.hero.sub' })}</p>
        <form
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            const text = q.trim();
            router.history.push(text ? `/search?q=${encodeURIComponent(text)}` : '/search');
          }}
        >
          <SearchField
            label={intl.formatMessage({ id: 'home.search.label' })}
            placeholder={intl.formatMessage({ id: 'home.search.placeholder' })}
            value={q}
            onChange={setQ}
            enterKeyHint="search"
          />
        </form>
      </header>
      <div className={styles.body}>
        <LimitBanner />
        <Link to="/baskets" className={`tz-card ${styles.basketEntry}`}>
          <Icon icon={ShoppingBasket} />
          <span className={styles.basketText}>
            <span className="tz-card-title">{intl.formatMessage({ id: 'basket.entry' })}</span>
            <span className="tz-meta">{intl.formatMessage({ id: 'basket.entry.body' })}</span>
          </span>
        </Link>
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
        {coords.status === 'ready' && <Nearby coords={coords.coords} category={category} />}
      </div>
      <CartBarLink />
    </>
  );
}

function Nearby({ coords, category }: { coords: Coords; category: string | undefined }) {
  const intl = useIntl();
  const router = useRouter();
  const [showMap, setShowMap] = useState(mapShown);
  const km = useRadiusKm();
  const cardProps = useStoreCardProps();
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
    queryKey: ['discover', 'stores', lat, lng, km, category ?? null],
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      listDiscoverStores({
        lat,
        lng,
        radius: radiusM(km),
        ...(category ? { category } : {}),
        limit: 20,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });

  const items = stores.data?.pages.flatMap((p) => p.items) ?? [];
  const names = useMemo(
    () => new Map((stores.data?.pages.flatMap((p) => p.items) ?? []).map((s) => [s.storeSlug, s.name] as const)),
    [stores.data],
  );
  const nextKm = RADIUS_OPTIONS_KM.find((k) => k > km);
  const num = (n: number) => n.toLocaleString('en-IN');

  return (
    <>
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

      <RadiusChips
        label={intl.formatMessage({ id: 'home.radius.label' })}
        options={RADIUS_OPTIONS_KM.map((k) => ({ km: k, label: intl.formatMessage({ id: 'home.radius.km' }, { km: String(k) }) }))}
        value={km}
        onChange={setRadiusKm}
      />

      {categories.data && categories.data.items.length > 0 && (
        <section className={styles.section} aria-label={intl.formatMessage({ id: 'home.categories.label' })}>
          <CategoryRow
            label={intl.formatMessage({ id: 'home.categories.label' })}
            LinkComponent={RouterLink}
            categories={categories.data.items.map((c) => ({
              key: c.slug,
              label: c.name,
              icon: categoryIcon(c.icon),
              tint: asTint(c.tint) ?? 'green',
              href: category === c.slug ? '/' : `/?category=${encodeURIComponent(c.slug)}`,
            }))}
          />
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
        <h2 id="home-shops" className={styles.sectionTitle}>
          {intl.formatMessage({ id: 'home.shops.title' })}
        </h2>
        <div aria-busy={stores.isPending} className={styles.list}>
          {stores.isPending && (
            <>
              <Skeleton shape="block" height="76px" />
              <Skeleton shape="block" height="76px" />
              <Skeleton shape="block" height="76px" />
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
        {stores.isSuccess && items.length === 0 && (
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
