// C-03 Home: where you are, what is around you (counts from the discover summary), the radius, categories
// and the shops near you. The full-screen map (C-04) and banners (W2-07) are later.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { getDiscoverSummary, listDiscoverStores } from '@todayz/api-client/gen/discover/discover';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useRouter } from '@tanstack/react-router';
import { CategoryRow, EmptyState, RadiusChips, SearchField, Skeleton, StoreCard, Button } from '@todayz/ui';
import { MapPin, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { usePlace } from '../app/place';
import { Icon } from '../shell/Icon';
import { CartBarLink } from '../store/CartBarLink';
import { categoryIcon } from './categories';
import { useCoords, type Coords } from './coords';
import { asTint, radiusM, useStoreCardProps } from './present';
import { RouterLink } from './RouterLink';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from './radius';
import styles from './home.module.css';

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
          <Link to="/location" className="tz-btn tz-btn-soft tz-btn-sm">
            <Icon icon={MapPin} />
            {where}
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
              <RouterLink href="/" className="tz-btn tz-btn-ghost tz-btn-sm">
                {intl.formatMessage({ id: 'home.category.clear' })}
              </RouterLink>
            </div>
          )}
        </section>
      )}

      {/* TODO(W2-08 follow-up): StoreMap card. Map tiles are not wired into the customer app's env and offline availability is unverified. */}

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
