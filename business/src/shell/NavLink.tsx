import { Link } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import type { NavItem } from './nav';

/**
 * One nav entry; TanStack Router sets aria-current="page" on the active link. `to` is the plain
 * path, so it keeps working when a WP replaces the `/$section` placeholder with a static route.
 * Overview is the landing page at `/` (B-03); its link matches `/` exactly, or it would be active
 * on every page.
 */
export function NavLink({ item }: { item: NavItem }) {
  const intl = useIntl();
  const label = intl.formatMessage({ id: item.label });
  if (item.section === 'overview') {
    return (
      <Link to="/" activeOptions={{ exact: true, includeSearch: false }}>
        <Icon icon={item.icon} />
        {label}
      </Link>
    );
  }
  return (
    <Link to="/$section" params={{ section: item.section }}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  );
}
