import { forwardRef, type CSSProperties, type HTMLAttributes } from 'react';
import { cx } from '../internal/cx';

export interface SkeletonProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'children' | 'aria-hidden'> {
  /** CSS length, e.g. "100%", "6ch", "48px". */
  width?: string | undefined;
  height?: string | undefined;
  /** text: rounded line; block: card-radius box; circle: avatar. */
  shape?: 'text' | 'block' | 'circle' | undefined;
  className?: string | undefined;
}

/**
 * Loading placeholder (no spinner for content). Decorative: the busy region around it carries
 * aria-busy. The shimmer stops when motion is reduced.
 */
export const Skeleton = forwardRef<HTMLSpanElement, SkeletonProps>(function Skeleton(
  { width = '100%', height = '1em', shape = 'text', className, style, ...rest },
  ref,
) {
  const size: CSSProperties = { ...style, width, height };
  return (
    <span
      ref={ref}
      className={cx('tz-ui-skeleton', `tz-ui-skeleton-${shape}`, className)}
      style={size}
      {...rest}
      // Always decorative, whatever is passed.
      aria-hidden="true"
    />
  );
});
