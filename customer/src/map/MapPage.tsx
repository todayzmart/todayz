// C-04 Full map: the same pins as the Home card, pannable, with the radius chips, a map or list switch and a
// sheet for the tapped shop. "Search this area" is not here: StoreMap has no map-moved event and the discover
// search is centred on the customer's place (TODO(lead) in the report).
import { getDiscoverStore, getDiscoverSummary, listDiscoverStores } from '@todayz/api-client/gen/discover/discover';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { formatDistance } from '@todayz/i18n';
import { Banner, Button, EmptyState, Segmented, Sheet, Skeleton, StoreCard } from '@todayz/ui';
import { ArrowLeft, MapPin } from 'lucide-react';
import { lazy, Suspense, useEffect, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { directionsUrl, radiusM, storeHref, useStoreCardProps } from '../home/present';
import { useRadiusKm } from '../home/radius';
import { RouterLink } from '../home/RouterLink';
import { Icon } from '../shell/Icon';
import { CartBarLink } from '../store/CartBarLink';
import styles from './map.module.css';

const StoreMapCard = lazy(() => import('../home/StoreMapCard'));

type View = 'map' | 'list';

export function MapPage() {
  const intl = useIntl();
  const coordsState = useCoords();
  const km = useRadiusKm();
  const cardProps = useStoreCardProps();
  const [view, setView] = useState<View>('map');
  const [slug, setSlug] = useState<string | null>(null);
  const [height, setHeight] = useState(() => mapHeight());
  useEffect(() => {
    const onResize = () => setHeight(mapHeight());
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  const coords = coordsState.status === 'ready' ? coordsState.coords : null;
  const summary = useQuery({
    queryKey: ['discover', 'summary', coords?.lat ?? null, coords?.lng ?? null, km],
    enabled: coords !== null,
    queryFn: () => getDiscoverSummary({ lat: coords?.lat ?? 0, lng: coords?.lng ?? 0, radius: radiusM(km) }),
  });
  const stores = useInfiniteQuery({
    queryKey: ['discover', 'stores', coords?.lat ?? null, coords?.lng ?? null, km, null],
    enabled: coords !== null,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      listDiscoverStores({
        lat: coords?.lat ?? 0,
        lng: coords?.lng ?? 0,
        radius: radiusM(km),
        limit: 20,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });
  const items = useMemo(() => stores.data?.pages.flatMap((p) => p.items) ?? [], [stores.data]);
  const names = useMemo(() => new Map(items.map((s) => [s.storeSlug, s.name] as const)), [items]);

  const picked = useQuery({
    queryKey: ['discover', 'store', slug, coords?.lat ?? null, coords?.lng ?? null],
    enabled: slug !== null,
    retry: false,
    queryFn: () => getDiscoverStore(slug ?? '', coords ? { lat: coords.lat, lng: coords.lng } : undefined),
  });

  return (
    <div className={styles.page}>
      <header className={styles.head}>
        <Link to="/" className="tz-btn tz-btn-ghost tz-btn-sm" aria-label={intl.formatMessage({ id: 'map.back' })}>
          <Icon icon={ArrowLeft} />
        </Link>
        <h1 className={styles.title}>{intl.formatMessage({ id: 'map.title' })}</h1>
        <Segmented<View>
          label={intl.formatMessage({ id: 'map.view.label' })}
          options={[
            { value: 'map', label: intl.formatMessage({ id: 'map.view.map' }) },
            { value: 'list', label: intl.formatMessage({ id: 'map.view.list' }) },
          ]}
          value={view}
          onChange={setView}
        />
      </header>

      {coordsState.status === 'none' && (
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
      {coordsState.status === 'loading' && <Skeleton shape="block" height="320px" />}
      {coordsState.status === 'error' && (
        <EmptyState
          icon={MapPin}
          title={intl.formatMessage({ id: 'home.placeError.title' })}
          description={intl.formatMessage({ id: 'home.placeError.body' })}
          action={<Button onClick={coordsState.retry}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
        />
      )}

      {coords && view === 'map' && (
        <>
          <Suspense fallback={<Skeleton shape="block" height={`${height}px`} />}>
            <StoreMapCard coords={coords} km={km} summary={summary.data} names={names} interactive height={height} onSelectShop={setSlug} />
          </Suspense>
          {summary.data?.pinsTruncated && (
            <Banner tone="info" live="none">
              {intl.formatMessage({ id: 'map.truncated' }, { count: String(summary.data.pins.length) })}
            </Banner>
          )}
          {summary.isError && (
            <Banner tone="warning" live="status">
              {intl.formatMessage({ id: 'home.shops.error' })}
            </Banner>
          )}
        </>
      )}

      {coords && view === 'list' && (
        <section className={styles.list} aria-busy={stores.isPending}>
          {stores.isPending && <Skeleton shape="block" height="76px" />}
          {items.map((s) => (
            <StoreCard key={s.storeSlug} {...cardProps(s)} />
          ))}
          {stores.isError && (
            <EmptyState
              title={intl.formatMessage({ id: 'home.shops.error' })}
              action={<Button onClick={() => void stores.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
            />
          )}
          {stores.hasNextPage && (
            <Button variant="soft" block loading={stores.isFetchingNextPage} onClick={() => void stores.fetchNextPage()}>
              {intl.formatMessage({ id: 'home.shops.more' })}
            </Button>
          )}
        </section>
      )}

      <Sheet
        open={slug !== null}
        onClose={() => setSlug(null)}
        title={picked.data?.name ?? names.get(slug ?? '') ?? intl.formatMessage({ id: 'home.map.shopFallback' })}
        description={
          picked.data
            ? [picked.data.distanceM !== null ? formatDistance(picked.data.distanceM) : null, picked.data.locality, intl.formatMessage({ id: picked.data.open ? 'store.open' : 'store.closed' })]
                .filter(Boolean)
                .join(' · ')
            : undefined
        }
        closeLabel={intl.formatMessage({ id: 'map.sheet.close' })}
        footer={
          picked.data ? (
            <>
              <a className="tz-btn tz-btn-ghost" href={directionsUrl(picked.data.lat, picked.data.lng)} target="_blank" rel="noreferrer">
                {intl.formatMessage({ id: 'map.sheet.directions' })}
              </a>
              <RouterLink className="tz-btn tz-btn-primary" href={storeHref(picked.data.storeSlug)}>
                {intl.formatMessage({ id: 'map.sheet.view' })}
              </RouterLink>
            </>
          ) : undefined
        }
      >
        {picked.isPending && slug !== null && <Skeleton width="60%" />}
        {picked.isError && <p>{intl.formatMessage({ id: 'map.sheet.error' })}</p>}
      </Sheet>
      <CartBarLink />
    </div>
  );
}

/** The map takes the screen below the header, never less than 280 px. */
function mapHeight(): number {
  return Math.max(280, Math.round(window.innerHeight - 260));
}
