// C-08 Product detail, built from what the shop page or a search result already returned (itemCache);
// a direct visit pages the shop's catalogue to find it. Other nearby shops come from searchDiscover.
import { getDiscoverStore, searchDiscover } from '@todayz/api-client/gen/discover/discover';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, EmptyState, QuantityStepper, Skeleton, StockTag, useToast } from '@todayz/ui';
import { formatDistance } from '@todayz/i18n';
import { ChevronLeft, Link2, MapPin, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { offPercent, useFmt, usePackLine, useStockProps } from '../home/format';
import { directionsUrl, initials, radiusM } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { useRadiusKm } from '../home/radius';
import { AskNearbySlot } from '../search/slots';
import { Icon } from '../shell/Icon';
import { CartBarLink } from '../store/CartBarLink';
import { itemHref, useStepperLabels } from '../store/itemUi';
import { findItem, itemKey, seedItems, type CachedItem } from '../store/itemCache';
import { useCartActions } from '../store/useCartActions';
import styles from './product.module.css';

export function ProductPage({ slug, itemId }: { slug: string; itemId: string }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const { money } = useFmt();
  const packLine = usePackLine();
  const stockProps = useStockProps();
  const stepperLabels = useStepperLabels();
  const coordsState = useCoords();
  const coords = coordsState.status === 'ready' ? coordsState.coords : null;
  const km = useRadiusKm();
  const { qtyOf, change, sheet } = useCartActions();
  const [qty, setQty] = useState(1);

  const found = useQuery<CachedItem | null>({
    queryKey: itemKey(slug, itemId),
    staleTime: 60_000,
    retry: false,
    queryFn: () => findItem(slug, itemId),
  });
  const shop = useQuery({
    queryKey: ['discover', 'store', slug, coords?.lat ?? null, coords?.lng ?? null],
    retry: false,
    queryFn: () => getDiscoverStore(slug, coords ? { lat: coords.lat, lng: coords.lng } : undefined),
  });

  const item = found.data?.item;
  const serverNow = found.data?.serverNow ?? '';

  // Other nearby shops: the discover search by name, keeping the results with the same product.
  // TODO(lead): the search has no productId filter, so this matches by name and filters on the client.
  const others = useQuery({
    queryKey: ['discover', 'others', item?.productId ?? null, coords?.lat ?? null, coords?.lng ?? null, km],
    enabled: item !== undefined && coords !== null,
    staleTime: 60_000,
    queryFn: async () => {
      if (!item || !coords) return [];
      const r = await searchDiscover({ q: item.name, lat: coords.lat, lng: coords.lng, radius: radiusM(km), limit: 20 });
      const out = r.results.flatMap((res) =>
        res.store.storeSlug === slug
          ? []
          : res.items.filter((i) => i.productId === item.productId && i.inStock).map((i) => ({ store: res.store, item: i, serverNow: r.serverNow })),
      );
      for (const o of out) seedItems(qc, o.store.storeSlug, [o.item], o.serverNow);
      return out;
    },
  });

  if (found.isPending) {
    return (
      <div className={styles.page}>
        <Skeleton shape="block" height="200px" />
        <Skeleton width="70%" height="28px" />
      </div>
    );
  }
  if (!item) {
    return (
      <div className={styles.page}>
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'product.gone.title' })}
          description={intl.formatMessage({ id: 'product.gone.body' })}
          action={
            <RouterLink href={`/s/${encodeURIComponent(slug)}`} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'product.gone.action' })}
            </RouterLink>
          }
        />
      </div>
    );
  }

  const shopName = shop.data?.name ?? slug;
  const bookable = shop.data?.bookable ?? true;
  const inCart = qtyOf(slug, item.storeItemId);
  const pct = offPercent(item.pricePaise, item.mrpPaise);
  const stock = stockProps(item, serverNow);
  const canAdd = bookable && item.inStock;

  const add = () => {
    change({ slug, name: shopName }, item, packLine(item), inCart + qty);
    toast.show({ message: intl.formatMessage({ id: 'product.added' }, { name: item.name }) });
  };

  // Share: plain link only for now (share cards are W3-16).
  const copyLink = () => {
    const url = `${window.location.origin}${itemHref(slug, item.storeItemId)}`;
    void navigator.clipboard
      ?.writeText(url)
      .then(() => toast.show({ message: intl.formatMessage({ id: 'product.share.copied' }) }))
      .catch(() => toast.show({ message: intl.formatMessage({ id: 'product.share.failed' }) }));
  };

  return (
    <div className={styles.page}>
      <RouterLink href={`/s/${encodeURIComponent(slug)}`} className={`tz-btn tz-btn-ghost tz-btn-sm ${styles.back}`}>
        <Icon icon={ChevronLeft} />
        {intl.formatMessage({ id: 'product.back' }, { shop: shopName })}
      </RouterLink>

      <div className={styles.media} aria-hidden="true">
        {initials(item.name)}
      </div>

      <div className={styles.titleBlock}>
        <h1 className={styles.name}>{item.name}</h1>
        <p className={styles.pack}>{packLine(item)}</p>
        {item.brand && <p className={styles.pack}>{item.brand}</p>}
      </div>

      <div className={styles.priceRow}>
        <span className="tz-price">{money(item.pricePaise)}</span>
        {pct !== null && item.mrpPaise !== null && (
          <>
            <span className="tz-mrp">{money(item.mrpPaise)}</span>
            <span className="tz-off">{intl.formatMessage({ id: 'product.off' }, { pct: String(pct) })}</span>
          </>
        )}
      </div>
      <StockTag {...stock} />

      <section className={styles.shop} aria-label={intl.formatMessage({ id: 'product.sold.by' })}>
        <RouterLink href={`/s/${encodeURIComponent(slug)}`} className="tz-ui-plainlink">
          <b>{shopName}</b>
        </RouterLink>
        <div className="tz-meta">
          {typeof shop.data?.distanceM === 'number' && <span>{formatDistance(shop.data.distanceM)}</span>}
          {shop.data?.locality && <span>{shop.data.locality}</span>}
        </div>
      </section>

      {canAdd ? (
        <div className={styles.buy}>
          <QuantityStepper
            quantity={qty}
            onChange={(n) => setQty(Math.max(1, n))}
            alwaysStepper
            labels={stepperLabels}
            itemName={item.name}
          />
          <Button variant="primary" onClick={add} block>
            {intl.formatMessage({ id: 'product.addToCart' })}
          </Button>
        </div>
      ) : !bookable && shop.data ? (
        <a className="tz-btn tz-btn-soft" href={directionsUrl(shop.data.lat, shop.data.lng)} target="_blank" rel="noopener noreferrer">
          <Icon icon={MapPin} />
          {intl.formatMessage({ id: 'store.directions' })}
        </a>
      ) : null}

      <div className={styles.actions}>
        <Button variant="ghost" size="sm" icon={Link2} onClick={copyLink}>
          {intl.formatMessage({ id: 'product.share' })}
        </Button>
      </div>

      {others.data && others.data.length > 0 && (
        <section className={styles.others} aria-labelledby="product-others">
          <h2 id="product-others" className={styles.sectionTitle}>
            {intl.formatMessage({ id: 'product.others.title' })}
          </h2>
          <ul className={styles.otherList}>
            {others.data.map((o) => (
              <li key={o.store.storeSlug}>
                <RouterLink href={itemHref(o.store.storeSlug, o.item.storeItemId)} className={styles.other}>
                  <span className="tz-ui-minw0">
                    <b>{o.store.name}</b>
                    <span className="tz-meta">
                      <span>{formatDistance(o.store.distanceM)}</span>
                    </span>
                  </span>
                  <span className="tz-price">{money(o.item.pricePaise)}</span>
                </RouterLink>
              </li>
            ))}
          </ul>
        </section>
      )}

      <AskNearbySlot query={item.name} />
      {sheet}
      <CartBarLink />
    </div>
  );
}
