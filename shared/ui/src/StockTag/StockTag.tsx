import { forwardRef, type HTMLAttributes, type ReactNode } from 'react';
import { cx } from '../internal/cx';

export type StockLevel = 'in' | 'low' | 'out';

/**
 * Proof of stock (D-101, 01 §11.1): "Confirmed {time} ago" while `stock_updated_at` is within
 * `stock_stale_after_hours`, then "Stock not confirmed recently" instead. The app decides staleness
 * from the setting and formats both strings; a stale tag needs its stale label and never shows the
 * old time.
 */
export type StockFreshness =
  | {
      stale?: false | undefined;
      /** Pre-formatted, e.g. "Confirmed 12 min ago". */
      freshness?: ReactNode;
      staleLabel?: ReactNode;
    }
  | {
      /** Older than `stock_stale_after_hours`. */
      stale: true;
      /** e.g. "Stock not confirmed recently"; replaces the freshness. */
      staleLabel: ReactNode;
      /** Ignored while stale. */
      freshness?: ReactNode;
    };

export type StockTagProps = {
  level: StockLevel;
  /** e.g. "In stock", "Only 3 left", "Out of stock". */
  label: ReactNode;
  /** inline: "In stock · Confirmed 12 min ago" on one line. stacked: freshness below (product card). */
  layout?: 'inline' | 'stacked' | undefined;
  className?: string | undefined;
} & StockFreshness &
  Omit<HTMLAttributes<HTMLDivElement>, 'children'>;

/** Stock state with its freshness (tz-stock + tz-fresh). */
export const StockTag = forwardRef<HTMLDivElement, StockTagProps>(function StockTag(
  { level, label, freshness, stale = false, staleLabel, layout = 'inline', className, ...rest },
  ref,
) {
  // Stale never falls back to the old time: that would claim a freshness the stock no longer has.
  const note = stale ? staleLabel : freshness;
  const stock = (
    <span className={cx('tz-stock', level === 'low' && 'tz-stock-low', level === 'out' && 'tz-stock-out')}>
      {label}
    </span>
  );
  if (layout === 'stacked') {
    return (
      <div ref={ref} className={className} data-stale={stale ? '' : undefined} {...rest}>
        {stock}
        {note !== undefined && <div className="tz-fresh">{note}</div>}
      </div>
    );
  }
  return (
    <div ref={ref} className={className} data-stale={stale ? '' : undefined} {...rest}>
      {stock}
      {note !== undefined && (
        <>
          {' '}
          <span className="tz-fresh">
            <span aria-hidden="true">· </span>
            {note}
          </span>
        </>
      )}
    </div>
  );
});
