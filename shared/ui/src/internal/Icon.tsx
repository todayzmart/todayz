import type { LucideIcon } from 'lucide-react';
import { cx } from './cx';

export type { LucideIcon };

export interface IconProps {
  icon: LucideIcon;
  /** 16px (dense UI) instead of the default 20px. */
  small?: boolean | undefined;
  className?: string | undefined;
}

/** A decorative Lucide icon on the design system's 1.5px stroke. Always aria-hidden. */
export function Icon({ icon: Glyph, small = false, className }: IconProps) {
  return (
    <Glyph
      className={cx('tz-ico', small && 'tz-ico-sm', className)}
      strokeWidth={1.5}
      aria-hidden="true"
      focusable="false"
    />
  );
}
