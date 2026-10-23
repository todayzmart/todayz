import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import { VisuallyHidden } from '../internal/VisuallyHidden';
import type { NavItem } from '../Sidebar/Sidebar';

export interface BottomNavProps {
  /** Accessible name, e.g. "Customer" or "Business". */
  label: string;
  /** Up to five destinations. */
  items: readonly NavItem[];
  current?: string | undefined;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Mobile bottom navigation (tz-bnav): icon over label; the current item is ink with a heavier stroke. */
export function BottomNav({ label, items, current, LinkComponent, className }: BottomNavProps) {
  const Link = LinkComponent ?? 'a';
  return (
    <nav className={cx('tz-bnav', className)} aria-label={label}>
      {items.map((it) => (
        <Link key={it.key} href={it.href} aria-current={it.key === current ? 'page' : undefined}>
          <span className="tz-ui-bnav-icon">
            <Icon icon={it.icon} />
            {it.count !== undefined && it.count > 0 && (
              <span className="tz-ui-bnav-badge tz-num" aria-hidden="true">
                {it.count > 99 ? '99+' : it.count}
              </span>
            )}
          </span>
          {it.label}
          {it.count !== undefined && it.count > 0 && <VisuallyHidden>, {it.countLabel ?? it.count}</VisuallyHidden>}
        </Link>
      ))}
    </nav>
  );
}
