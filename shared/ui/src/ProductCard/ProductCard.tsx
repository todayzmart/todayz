import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import type { LinkComponent } from '../internal/link';
import { QuantityStepper, type QuantityStepperLabels } from '../QuantityStepper/QuantityStepper';
import { StockTag, type StockLevel } from '../StockTag/StockTag';

export interface ProductCardProps {
  name: string;
  /** e.g. "5 kg". */
  unit?: ReactNode;
  /** Two-letter fallback shown on the media tile when there is no photo, e.g. "AT". */
  initials: string;
  image?: { src: string; alt?: string | undefined } | undefined;
  stock: {
    level: StockLevel;
    label: ReactNode;
    freshness?: ReactNode;
    stale?: boolean | undefined;
    staleLabel?: ReactNode;
  };
  /** Pre-formatted, e.g. "₹289". */
  price: ReactNode;
  /** Pre-formatted MRP, shown struck through when above the price. */
  mrp?: ReactNode;
  /** e.g. "10% off". */
  off?: ReactNode;
  quantity: number;
  onQuantityChange: (quantity: number) => void;
  /** Stock on hand; the + button stops there and Add is disabled at 0. */
  max?: number | undefined;
  stepperLabels: QuantityStepperLabels;
  /** Product page; the name becomes the link. */
  href?: string | undefined;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Product tile for search and shop pages (tz-pcard): media, name, unit, stock, price and stepper. */
export function ProductCard({
  name,
  unit,
  initials,
  image,
  stock,
  price,
  mrp,
  off,
  quantity,
  onQuantityChange,
  max,
  stepperLabels,
  href,
  LinkComponent,
  className,
}: ProductCardProps) {
  const Link = LinkComponent ?? 'a';
  const soldOut = stock.level === 'out';
  return (
    <article className={cx('tz-pcard', className)}>
      <div className="tz-thumb">
        {image ? (
          <img className="tz-ui-thumb-img" src={image.src} alt={image.alt ?? ''} loading="lazy" decoding="async" />
        ) : (
          <span aria-hidden="true">{initials}</span>
        )}
      </div>
      <div className="tz-pcard-name">
        {href ? (
          <Link href={href} className="tz-ui-plainlink">
            {name}
          </Link>
        ) : (
          name
        )}
      </div>
      {unit !== undefined && <div className="tz-pcard-unit">{unit}</div>}
      <StockTag
        layout="stacked"
        level={stock.level}
        label={stock.label}
        freshness={stock.freshness}
        stale={stock.stale}
        staleLabel={stock.staleLabel}
      />
      <div className="tz-pcard-foot">
        <div>
          <div className="tz-price">{price}</div>
          {mrp !== undefined ? (
            <>
              <span className="tz-mrp">{mrp}</span>
              {off !== undefined && (
                <>
                  {' '}
                  <span className="tz-off">{off}</span>
                </>
              )}
            </>
          ) : (
            // Keeps the price row the same height as cards that show an MRP.
            <span className="tz-mrp tz-ui-mrp-spacer" aria-hidden="true">
              {' '}
            </span>
          )}
        </div>
        <QuantityStepper
          quantity={quantity}
          onChange={onQuantityChange}
          max={soldOut ? 0 : max}
          labels={stepperLabels}
          itemName={name}
          disabled={soldOut && quantity === 0}
        />
      </div>
    </article>
  );
}
