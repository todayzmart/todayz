// One item of a shop as it appears on the shop page, in search results and on "other shops": price, MRP,
// stock with its proof, and Add to cart. An in-store-only shop (not bookable) shows directions instead of Add.
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import { MapPin, Store as StoreIcon } from 'lucide-react';
import { ProductCard, StockTag } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { offPercent, useFmt, usePackLine, useStockProps } from '../home/format';
import { initials } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { Icon } from '../shell/Icon';
import { itemHref, useStepperLabels } from './itemUi';

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
}

export function ItemCard({ shopSlug, item, serverNow, bookable, qty, onChange, directionsHref }: ItemCardProps) {
  const intl = useIntl();
  const { money } = useFmt();
  const packLine = usePackLine();
  const stockProps = useStockProps();
  const stepperLabels = useStepperLabels();
  const pct = offPercent(item.pricePaise, item.mrpPaise);
  const stock = stockProps(item, serverNow);
  const href = itemHref(shopSlug, item.storeItemId);

  if (!bookable) {
    return (
      <article className="tz-pcard">
        <div className="tz-thumb">
          <span aria-hidden="true">{initials(item.name)}</span>
        </div>
        <div className="tz-pcard-name">
          <RouterLink href={href} className="tz-ui-plainlink">
            {item.name}
          </RouterLink>
        </div>
        <div className="tz-pcard-unit">{packLine(item)}</div>
        <StockTag layout="stacked" {...stock} />
        <div className="tz-pcard-foot">
          <div>
            <div className="tz-price">{money(item.pricePaise)}</div>
          </div>
          {directionsHref ? (
            <a className="tz-btn tz-btn-soft tz-btn-sm" href={directionsHref} target="_blank" rel="noopener noreferrer">
              <Icon icon={MapPin} />
              {intl.formatMessage({ id: 'store.directions' })}
            </a>
          ) : (
            <RouterLink className="tz-btn tz-btn-soft tz-btn-sm" href={`/s/${encodeURIComponent(shopSlug)}`}>
              <Icon icon={StoreIcon} />
              {intl.formatMessage({ id: 'store.viewShop' })}
            </RouterLink>
          )}
        </div>
      </article>
    );
  }

  return (
    <ProductCard
      name={item.name}
      unit={packLine(item)}
      initials={initials(item.name)}
      stock={stock}
      price={money(item.pricePaise)}
      {...(pct !== null && item.mrpPaise !== null
        ? { mrp: money(item.mrpPaise), off: intl.formatMessage({ id: 'product.off' }, { pct: String(pct) }) }
        : {})}
      quantity={qty}
      onQuantityChange={onChange}
      stepperLabels={stepperLabels}
      href={href}
      LinkComponent={RouterLink}
    />
  );
}
