import { type ReactNode } from 'react';
import { cx } from '../internal/cx';

import type { PillTone } from './orderStatus';

export interface StatusPillProps {
  tone?: PillTone | undefined;
  /** Sentence-case status text, e.g. "Ready for pickup". */
  children: ReactNode;
  className?: string | undefined;
}

/** Status pill (tz-pill): colour lives only in the dot, so the text always carries the meaning. */
export function StatusPill({ tone = 'neutral', children, className }: StatusPillProps) {
  return <span className={cx('tz-pill', tone !== 'neutral' && `tz-pill-${tone}`, className)}>{children}</span>;
}
