// One item of a shop as it appears on the shop page, in search results, on "other shops" and in the
// Home rails: a square photo on a pastel tile with Add (then − n +) on its corner, the price with the
// struck MRP, the name, the pack and the stock with its proof (D-101). An in-store-only shop (not
// bookable) shows directions instead of Add. "Add to basket" (C-22) is the small icon on the photo; an
// out-of-stock item also offers similar ones (AI-C3).
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import { MapPin, Store as StoreIcon } from 'lucide-react';
import { QuantityStepper, StockTag } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { AddToBasket } from '../basket/AddToBasket';
import { offPercent, useFmt, usePackLine, useStockProps } from '../home/format';
import { imageUrl, initials } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { Substitutes } from '../substitutes/Substitutes';
import { Icon } from '../shell/Icon';
import { itemHref, tintOf, useStepperLabels } from './itemUi';
import styles from './tile.module.css';

export interface ItemCardProps {
  shopSlug: string;
  item: DiscoverSearchItem;
  serverNow: string;
  /** False for an in-store-only shop: no Add, directions or the shop page instead. */
  bookable: boolean;
  qty: number;
  onChange: (qty: number) => void;
  /** Where "Get directions" goes; without it the card links to the shop page. */
  directionsHref?: string | undefined;
  /** The shop's name under the item, for rails that mix shops. */
  shopName?: string | undefined;
}

export function ItemCard({ shopSlug, item, serverNow, bookable, qty, onChange, directionsHref, shopName }: ItemCardProps) {
  const intl = useIntl();
  const { money } = useFmt();
  const packLine = usePackLine();
  const stockProps = useStockProps();
  const stepperLabels = useStepperLabels();
  const pct = offPercent(item.pricePaise, item.mrpPaise);
  const stock = stockProps(item, serverNow);
  const href = itemHref(shopSlug, item.storeItemId);
  const img = imageUrl(item.imageKey);
  const out = !item.inStock;

  return (
    <article className={styles.tile} data-out={out ? '' : undefined}>
      <div className={styles.media} data-tint={tintOf(item.productId)}>
        {img ? (
          <img className={styles.img} src={img} alt="" loading="lazy" decoding="async" />
        ) : (
          <span className={styles.initials} aria-hidden="true">
            {initials(item.name)}
          </span>
        )}
        {pct !== null && <span className={styles.off}>{intl.formatMessage({ id: 'product.off' }, { pct: String(pct) })}</span>}
        {bookable && (
          <div className={styles.add}>
            <QuantityStepper
              quantity={qty}
              onChange={onChange}
              max={out ? 0 : undefined}
              labels={stepperLabels}
              itemName={item.name}
              disabled={out && qty === 0}
              size="sm"
            />
          </div>
        )}
        <div className={styles.basket}>
          <AddToBasket productId={item.productId} name={item.name} detail={packLine(item)} compact iconOnly />
        </div>
      </div>
      <div className={styles.info}>
        <div className={styles.priceRow}>
          <span className={styles.price}>{money(item.pricePaise)}</span>
          {pct !== null && item.mrpPaise !== null && <s className={styles.mrp}>{money(item.mrpPaise)}</s>}
        </div>
        <RouterLink href={href} className={styles.name}>
          {item.name}
        </RouterLink>
        <span className={styles.pack}>{packLine(item)}</span>
        {shopName && <span className={styles.shop}>{shopName}</span>}
        <StockTag layout="stacked" className={styles.stock} {...stock} />
        {out && <Substitutes shopSlug={shopSlug} storeItemId={item.storeItemId} name={item.name} />}
        {!bookable &&
          (directionsHref ? (
            <a className={`tz-btn tz-btn-soft tz-btn-sm ${styles.alt}`} href={directionsHref} target="_blank" rel="noopener noreferrer">
              <Icon icon={MapPin} />
              {intl.formatMessage({ id: 'store.directions' })}
            </a>
          ) : (
            <RouterLink className={`tz-btn tz-btn-soft tz-btn-sm ${styles.alt}`} href={`/s/${encodeURIComponent(shopSlug)}`}>
              <Icon icon={StoreIcon} />
              {intl.formatMessage({ id: 'store.viewShop' })}
            </RouterLink>
          ))}
      </div>
    </article>
  );
}
