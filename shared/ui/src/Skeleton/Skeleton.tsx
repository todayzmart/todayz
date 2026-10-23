import { type CSSProperties } from 'react';
import { cx } from '../internal/cx';

export interface SkeletonProps {
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
export function Skeleton({ width = '100%', height = '1em', shape = 'text', className }: SkeletonProps) {
  const style: CSSProperties = { width, height };
  return (
    <span className={cx('tz-ui-skeleton', `tz-ui-skeleton-${shape}`, className)} style={style} aria-hidden="true" />
  );
}
