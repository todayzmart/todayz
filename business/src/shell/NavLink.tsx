import { Link } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import type { NavItem } from './nav';

/**
 * One nav entry; TanStack Router sets aria-current="page" on the active link. `to` is the plain
 * path, so it keeps working when a WP replaces the `/$section` placeholder with a static route.
 */
export function NavLink({ item }: { item: NavItem }) {
  const intl = useIntl();
  const label = intl.formatMessage({ id: item.label });
  return (
    <Link to="/$section" params={{ section: item.section }}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  );
}
