import { ChevronRight } from 'lucide-react';
import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import { VisuallyHidden } from '../internal/VisuallyHidden';

export interface CartBarProps {
  /** Number of items, shown in the badge. */
  count: number;
  /** Read instead of the bare number, e.g. "3 items". */
  countLabel: string;
  /** Pre-formatted cart total, e.g. "₹345". */
  total: ReactNode;
  /** e.g. "from Sri Lakshmi Provision Stores". */
  from?: ReactNode;
  ctaLabel: string;
  onOpen: () => void;
  className?: string | undefined;
}

/**
 * Floating brand cart bar (tz-cartbar). The app positions it above the bottom nav; the count and
 * total are announced politely when they change.
 */
export function CartBar({ count, countLabel, total, from, ctaLabel, onOpen, className }: CartBarProps) {
  return (
    <div className={cx('tz-cartbar', className)}>
      <span className="tz-cartbar-count" aria-hidden="true">
        {count}
      </span>
      <span className="tz-cartbar-text" aria-live="polite" aria-atomic="true">
        <VisuallyHidden>{countLabel}, </VisuallyHidden>
        {total}
        {from !== undefined && <small>{from}</small>}
      </span>
      <button type="button" className="tz-cartbar-cta" onClick={onOpen}>
        {ctaLabel}
        <Icon icon={ChevronRight} small />
      </button>
    </div>
  );
}
