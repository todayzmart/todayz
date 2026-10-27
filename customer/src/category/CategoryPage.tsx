// C-06 Category page: the shops of one category within the radius (`category` takes in a parent's children).
// Products of a category are not listed: the discover search needs text and has no category filter.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { listDiscoverStores } from '@todayz/api-client/gen/discover/discover';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Button, EmptyState, RadiusChips, Skeleton, StoreCard } from '@todayz/ui';
import { ArrowLeft, MapPin, Store as StoreIcon } from 'lucide-react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { radiusM, useStoreCardProps } from '../home/present';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from '../home/radius';
import { Icon } from '../shell/Icon';
import { CartBarLink } from '../store/CartBarLink';
import homeStyles from '../home/home.module.css';

export function CategoryPage({ slug }: { slug: string }) {
  const intl = useIntl();
  const coordsState = useCoords();
  const km = useRadiusKm();
  const cardProps = useStoreCardProps();
  const coords = coordsState.status === 'ready' ? coordsState.coords : null;

  const categories = useQuery({
    queryKey: ['catalogue', 'categories'],
    staleTime: 60 * 60_000,
    queryFn: () => listCategories(),
  });
  const category = categories.data?.items.flatMap((c) => [c, ...c.children]).find((c) => c.slug === slug);

  const stores = useInfiniteQuery({
    queryKey: ['discover', 'stores', coords?.lat ?? null, coords?.lng ?? null, km, slug],
    enabled: coords !== null,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      listDiscoverStores({
        lat: coords?.lat ?? 0,
        lng: coords?.lng ?? 0,
        radius: radiusM(km),
        category: slug,
        limit: 20,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });
  const items = stores.data?.pages.flatMap((p) => p.items) ?? [];
  const nextKm = RADIUS_OPTIONS_KM.find((k) => k > km);
  const unknown = categories.isSuccess && !category;

  return (
    <div className={homeStyles.body}>
      <Link to="/" className="tz-btn tz-btn-ghost tz-btn-sm">
        <Icon icon={ArrowLeft} />
        {intl.formatMessage({ id: 'category.home' })}
      </Link>

      {unknown ? (
        <EmptyState icon={StoreIcon} title={intl.formatMessage({ id: 'category.notFound.title' })} description={intl.formatMessage({ id: 'category.notFound.body' })} />
      ) : (
        <>
          <h1 className="tz-page-title">
            {category ? intl.formatMessage({ id: 'category.shops' }, { name: category.name }) : <Skeleton width="60%" />}
          </h1>
          <RadiusChips
            label={intl.formatMessage({ id: 'home.radius.label' })}
            options={RADIUS_OPTIONS_KM.map((k) => ({ km: k, label: intl.formatMessage({ id: 'home.radius.km' }, { km: String(k) }) }))}
            value={km}
            onChange={setRadiusKm}
          />
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
          {coordsState.status === 'error' && (
            <EmptyState
              icon={MapPin}
              title={intl.formatMessage({ id: 'home.placeError.title' })}
              description={intl.formatMessage({ id: 'home.placeError.body' })}
              action={<Button onClick={coordsState.retry}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
            />
          )}
          {coords && (
            <div aria-busy={stores.isPending} className={homeStyles.list}>
              {stores.isPending && (
                <>
                  <Skeleton shape="block" height="76px" />
                  <Skeleton shape="block" height="76px" />
                </>
              )}
              {items.map((s) => (
                <StoreCard key={s.storeSlug} {...cardProps(s)} />
              ))}
              {stores.isError && (
                <EmptyState
                  title={intl.formatMessage({ id: 'home.shops.error' })}
                  action={<Button onClick={() => void stores.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
                />
              )}
              {stores.isSuccess && items.length === 0 && (
                <EmptyState
                  icon={StoreIcon}
                  title={intl.formatMessage({ id: 'category.empty.title' }, { name: category?.name ?? slug, km: String(km) })}
                  description={intl.formatMessage({ id: 'category.empty.body' })}
                  action={
                    nextKm !== undefined ? (
                      <Button onClick={() => setRadiusKm(nextKm)}>{intl.formatMessage({ id: 'home.empty.widen' }, { km: String(nextKm) })}</Button>
                    ) : undefined
                  }
                />
              )}
              {stores.hasNextPage && (
                <Button variant="soft" block loading={stores.isFetchingNextPage} onClick={() => void stores.fetchNextPage()}>
                  {intl.formatMessage({ id: 'home.shops.more' })}
                </Button>
              )}
            </div>
          )}
        </>
      )}
      <CartBarLink />
    </div>
  );
}
