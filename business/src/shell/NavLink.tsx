import { VisuallyHidden } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import type { NavItem } from './nav';

/** Neutral sidebar badge (design `.tz-count`), as the kit Sidebar draws it: the number is hidden
 *  from screen readers and `label` is read instead ("Orders, 2 orders waiting"). */
export interface NavCount {
  count: number;
  label: string;
}

/**
 * One nav entry; TanStack Router sets aria-current="page" on the active link. `to` is the plain
 * path, so it keeps working when a WP replaces the `/$section` placeholder with a static route.
 * Overview is the landing page at `/` (B-03); its link matches `/` exactly, or it would be active
 * on every page. A zero or missing count draws no badge.
 */
export function NavLink({ item, count }: { item: NavItem; count?: NavCount | undefined }) {
  const intl = useIntl();
  const label = intl.formatMessage({ id: item.label });
  const badge = count && count.count > 0 && (
    <>
      <span className="tz-count" aria-hidden="true">
        {intl.formatNumber(count.count)}
      </span>
      <VisuallyHidden>, {count.label}</VisuallyHidden>
    </>
  );
  if (item.section === 'overview') {
    return (
      <Link to="/" activeOptions={{ exact: true, includeSearch: false }}>
        <Icon icon={item.icon} />
        {label}
        {badge}
      </Link>
    );
  }
  return (
    <Link to="/$section" params={{ section: item.section }}>
      <Icon icon={item.icon} />
      {label}
      {badge}
    </Link>
  );
}
