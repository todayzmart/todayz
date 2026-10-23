import { Info, TriangleAlert } from 'lucide-react';
import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export interface BannerProps {
  tone?: 'info' | 'warning' | undefined;
  /** Bold lead-in, e.g. "You're offline." */
  title?: ReactNode;
  children?: ReactNode;
  icon?: LucideIcon | undefined;
  /** Optional trailing action (a small button or link). */
  action?: ReactNode;
  /**
   * status (polite) for changes that appear while the page is open, such as going offline;
   * none for banners rendered with the page.
   */
  live?: 'status' | 'none' | undefined;
  className?: string | undefined;
}

/** Inline page banner (tz-banner): info (sky) or warning (amber). Never a modal. */
export function Banner({ tone = 'info', title, children, icon, action, live = 'none', className }: BannerProps) {
  return (
    <div
      className={cx('tz-banner', tone === 'warning' && 'tz-banner-warning', className)}
      role={live === 'status' ? 'status' : undefined}
    >
      <Icon icon={icon ?? (tone === 'warning' ? TriangleAlert : Info)} />
      <span className="tz-ui-grow">
        {title !== undefined && <b>{title}</b>}
        {title !== undefined && children !== undefined && ' '}
        {children}
      </span>
      {action}
    </div>
  );
}
