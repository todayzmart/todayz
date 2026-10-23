import { forwardRef, type HTMLAttributes, type ReactNode } from 'react';
import { cx } from '../internal/cx';

import type { PillTone } from './orderStatus';

export interface StatusPillProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'children'> {
  tone?: PillTone | undefined;
  /** Sentence-case status text, e.g. "Ready for pickup". */
  children: ReactNode;
  className?: string | undefined;
}

/** Status pill (tz-pill): colour lives only in the dot, so the text always carries the meaning. */
export const StatusPill = forwardRef<HTMLSpanElement, StatusPillProps>(function StatusPill(
  { tone = 'neutral', children, className, ...rest },
  ref,
) {
  return (
    <span ref={ref} className={cx('tz-pill', tone !== 'neutral' && `tz-pill-${tone}`, className)} {...rest}>
      {children}
    </span>
  );
});
