import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import { VisuallyHidden } from '../internal/VisuallyHidden';

export interface NavItem {
  key: string;
  label: ReactNode;
  href: string;
  icon: LucideIcon;
  /** Small count, e.g. orders waiting. */
  count?: number | undefined;
  /** Read with the count, e.g. "3 waiting". Without it the bare number is read. */
  countLabel?: string | undefined;
}

export interface NavGroup {
  key: string;
  /** Group heading, e.g. "Grow". Omit for the first, unlabelled group. */
  label?: ReactNode;
  items: readonly NavItem[];
}

export interface SidebarProps {
  /** Accessible name of the navigation landmark, e.g. "Business". */
  label: string;
  groups: readonly NavGroup[];
  /** Key of the current page's item (aria-current="page"). */
  current?: string | undefined;
  /** Brand row; defaults to the TodayZ wordmark. */
  brand?: ReactNode;
  /** Content pinned to the bottom, e.g. the store switcher card (tz-side-card). */
  footer?: ReactNode;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** Desktop sidebar (tz-side) for the business app and admin console. */
export function Sidebar({ label, groups, current, brand, footer, LinkComponent, className }: SidebarProps) {
  const Link = LinkComponent ?? 'a';
  return (
    <nav className={cx('tz-side', className)} aria-label={label}>
      <div className="tz-side-brand">
        {brand ?? (
          <span>
            Today<b>Z</b>
          </span>
        )}
      </div>
      {groups.map((g) => (
        <SidebarGroup key={g.key} group={g} current={current} Link={Link} />
      ))}
      {footer}
    </nav>
  );
}

function SidebarGroup({
  group,
  current,
  Link,
}: {
  group: NavGroup;
  current: string | undefined;
  Link: LinkComponent | 'a';
}) {
  return (
    <>
      {group.label !== undefined && <div className="tz-side-group">{group.label}</div>}
      {group.items.map((it) => (
        <Link key={it.key} href={it.href} aria-current={it.key === current ? 'page' : undefined}>
          <Icon icon={it.icon} />
          {it.label}
          {it.count !== undefined && it.count > 0 && (
            <>
              <span className="tz-count" aria-hidden="true">
                {it.count}
              </span>
              <VisuallyHidden>, {it.countLabel ?? it.count}</VisuallyHidden>
            </>
          )}
        </Link>
      ))}
    </>
  );
}
