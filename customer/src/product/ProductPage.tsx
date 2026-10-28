// C-08 Product detail, built from what the shop page or a search result already returned (itemCache);
// a direct visit pages the shop's catalogue to find it. Other nearby shops come from searchDiscover.
import { getDiscoverStore, searchDiscover } from '@todayz/api-client/gen/discover/discover';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, EmptyState, QuantityStepper, Skeleton, StockTag, useToast } from '@todayz/ui';
import { formatDistance } from '@todayz/i18n';
import { ChevronLeft, MapPin, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { offPercent, useFmt, usePackLine, useStockProps } from '../home/format';
import { directionsUrl, imageUrl, initials, radiusM } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { useRadiusKm } from '../home/radius';
import { AddToBasket } from '../basket/AddToBasket';
import { AskNearbySlot } from '../search/slots';
import { ShareButton } from '../share/ShareButton';
import { Icon } from '../shell/Icon';
import { Substitutes } from '../substitutes/Substitutes';
import { CartBarLink } from '../store/CartBarLink';
import { itemHref, tintOf, useStepperLabels } from '../store/itemUi';
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

  // Other nearby shops: the discover search limited to this master product (`productId`; `q` stays required).
  // A shop that lists it but has run out stays in the list, after the ones with stock, labelled (D-282).
  const others = useQuery({
    queryKey: ['discover', 'others', item?.productId ?? null, coords?.lat ?? null, coords?.lng ?? null, km],
    enabled: item !== undefined && coords !== null,
    staleTime: 60_000,
    queryFn: async () => {
      if (!item || !coords) return [];
      const r = await searchDiscover({ q: item.name, productId: item.productId, lat: coords.lat, lng: coords.lng, radius: radiusM(km), limit: 20 });
      const out = r.results.flatMap((res) =>
        res.store.storeSlug === slug
          ? []
          : res.items.filter((i) => i.productId === item.productId).map((i) => ({ store: res.store, item: i, serverNow: r.serverNow })),
      );
      out.sort((a, b) => Number(b.item.inStock) - Number(a.item.inStock));
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
  const canAdd = bookable && item.inStock && shop.data !== undefined;

  const add = () => {
    if (!shop.data) return;
    change({ id: shop.data.storeId, slug, name: shopName }, item, packLine(item), inCart + qty);
    toast.show({ message: intl.formatMessage({ id: 'product.added' }, { name: item.name }) });
  };

  return (
    <div className={styles.page}>
      <RouterLink href={`/s/${encodeURIComponent(slug)}`} className={`tz-btn tz-btn-ghost tz-btn-sm ${styles.back}`}>
        <Icon icon={ChevronLeft} />
        {intl.formatMessage({ id: 'product.back' }, { shop: shopName })}
      </RouterLink>

      <div className={styles.layout}>
        <div className={styles.gallery}>
          <div className={styles.media} data-tint={tintOf(item.productId)} aria-hidden="true">
            {imageUrl(item.imageKey) ? <img className={styles.mediaImg} src={imageUrl(item.imageKey)} alt="" decoding="async" /> : initials(item.name)}
          </div>
          {imageUrl(item.imageKey) && item.imageCredit && (
            // An open-database photo is CC BY-SA: name the authors, link the source and the licence (D-252).
            <p className={styles.credit}>
              {intl.formatMessage(
                { id: 'product.photoCredit' },
                {
                  author: (
                    <a href={item.imageCredit.sourceUrl} target="_blank" rel="noopener noreferrer">
                      {item.imageCredit.author}
                    </a>
                  ),
                  licence: (
                    <a href={item.imageCredit.licenceUrl} target="_blank" rel="noopener noreferrer license">
                      {item.imageCredit.licence}
                    </a>
                  ),
                },
              )}
            </p>
          )}
        </div>

        <div className={styles.details}>
          <div className={styles.titleBlock}>
            <h1 className={styles.name}>{item.name}</h1>
            <p className={styles.pack}>{packLine(item)}</p>
            {item.brand && <p className={styles.pack}>{item.brand}</p>}
          </div>

          <div className={styles.priceRow}>
            <span className={styles.price}>{money(item.pricePaise)}</span>
            {pct !== null && item.mrpPaise !== null && (
              <>
                <span className="tz-mrp">{money(item.mrpPaise)}</span>
                <span className={styles.off}>{intl.formatMessage({ id: 'product.off' }, { pct: String(pct) })}</span>
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

          <section className={styles.infoBlock} aria-labelledby="product-highlights">
            <h2 id="product-highlights" className={styles.sectionTitle}>
              {intl.formatMessage({ id: 'product.highlights.title' })}
            </h2>
            <ul className={styles.highlights}>
              <li>
                {intl.formatMessage(
                  { id: 'product.highlights.genuine' },
                  { brand: item.brand ?? intl.formatMessage({ id: 'product.details.unbranded' }) },
                )}
              </li>
              <li>{intl.formatMessage({ id: 'product.highlights.neighbourhood' })}</li>
              <li>{intl.formatMessage({ id: 'product.highlights.fulfilment' })}</li>
            </ul>
          </section>

          <section className={styles.infoBlock} aria-labelledby="product-about">
            <h2 id="product-about" className={styles.sectionTitle}>
              {intl.formatMessage({ id: 'product.about.title' })}
            </h2>
            <p className={styles.about}>
              {intl.formatMessage(
                { id: 'product.about.body' },
                {
                  name: item.name,
                  brand: item.brand ?? intl.formatMessage({ id: 'product.details.unbranded' }),
                  pack: packLine(item),
                  shop: shopName,
                },
              )}
            </p>
          </section>

          <section className={styles.infoBlock} aria-labelledby="product-specs">
            <h2 id="product-specs" className={styles.sectionTitle}>
              {intl.formatMessage({ id: 'product.details.title' })}
            </h2>
            <dl className={styles.specs}>
              <div>
                <dt>{intl.formatMessage({ id: 'product.details.brand' })}</dt>
                <dd>{item.brand ?? intl.formatMessage({ id: 'product.details.unbranded' })}</dd>
              </div>
              <div>
                <dt>{intl.formatMessage({ id: 'product.details.net' })}</dt>
                <dd>{packLine(item)}</dd>
              </div>
              <div>
                <dt>{intl.formatMessage({ id: 'product.details.seller' })}</dt>
                <dd>{shopName}</dd>
              </div>
              <div>
                <dt>{intl.formatMessage({ id: 'product.details.origin' })}</dt>
                <dd>{intl.formatMessage({ id: 'product.details.originIndia' })}</dd>
              </div>
            </dl>
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
          {!item.inStock && <Substitutes shopSlug={slug} storeItemId={item.storeItemId} name={item.name} size="md" />}

          <div className={styles.actions}>
            <AddToBasket productId={item.productId} name={item.name} detail={packLine(item)} />
            <ShareButton storeItemId={item.storeItemId} />
          </div>
        </div>
      </div>

      {others.data && others.data.length > 0 && (
        <section className={styles.others} aria-labelledby="product-others">
          <h2 id="product-others" className={styles.sectionTitle}>
            {intl.formatMessage({ id: 'product.others.title' })}
          </h2>
          <ul className={styles.otherList}>
            {others.data.map((o) => (
              <li key={o.item.storeItemId}>
                <RouterLink href={itemHref(o.store.storeSlug, o.item.storeItemId)} className={styles.other}>
                  <span className="tz-ui-minw0">
                    <b>{o.store.name}</b>
                    <span className="tz-meta">
                      <span>{formatDistance(o.store.distanceM)}</span>
                      {!o.item.inStock && <span>{intl.formatMessage({ id: 'product.stock.out' })}</span>}
                    </span>
                  </span>
                  <span className="tz-price">{money(o.item.pricePaise)}</span>
                </RouterLink>
              </li>
            ))}
          </ul>
        </section>
      )}

      <AskNearbySlot query={item.name} productId={item.productId} />
      {sheet}
      <CartBarLink />
    </div>
  );
}
