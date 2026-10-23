import { type ReactNode } from 'react';
import { cx } from '../internal/cx';

export type StockLevel = 'in' | 'low' | 'out';

export interface StockTagProps {
  level: StockLevel;
  /** e.g. "In stock", "Only 3 left", "Out of stock". */
  label: ReactNode;
  /** Pre-formatted freshness, e.g. "updated 12 min ago". Customers always see it (01 §4). */
  freshness?: ReactNode;
  /**
   * The item is older than `stock_stale_after_hours`: `staleLabel` ("Stock not confirmed
   * recently") replaces the freshness. The app decides staleness from the setting.
   */
  stale?: boolean | undefined;
  staleLabel?: ReactNode;
  /** inline: "In stock · updated 12 min ago" on one line. stacked: freshness below (product card). */
  layout?: 'inline' | 'stacked' | undefined;
  className?: string | undefined;
}

/** Stock state with its freshness (tz-stock + tz-fresh). */
export function StockTag({
  level,
  label,
  freshness,
  stale = false,
  staleLabel,
  layout = 'inline',
  className,
}: StockTagProps) {
  const note = stale && staleLabel !== undefined ? staleLabel : freshness;
  const stock = (
    <span className={cx('tz-stock', level === 'low' && 'tz-stock-low', level === 'out' && 'tz-stock-out')}>
      {label}
    </span>
  );
  if (layout === 'stacked') {
    return (
      <div className={className} data-stale={stale ? '' : undefined}>
        {stock}
        {note !== undefined && <div className="tz-fresh">{note}</div>}
      </div>
    );
  }
  return (
    <div className={className} data-stale={stale ? '' : undefined}>
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
}
