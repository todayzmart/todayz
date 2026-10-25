import { useSession } from '@todayz/auth';
import { useQueryClient } from '@tanstack/react-query';
import { Link, Navigate, Outlet, useRouterState } from '@tanstack/react-router';
import { Button, Skeleton } from '@todayz/ui';
import { LogOut } from 'lucide-react';
import { Fragment } from 'react';
import { useIntl } from 'react-intl';
import { canOpen, useMe } from '../app/nav/access';
import { Icon } from './Icon';
import { NoAccess, AccessFailed } from './NoAccess';
import { NAV, navItem, type NavItem } from './nav';
import styles from './shell.module.css';

/**
 * One sidebar entry. The visible label is the accessible name (visually hidden on the icon rail);
 * `title` gives the rail a hover tooltip.
 */
function NavLink({ item }: { item: NavItem }) {
  const intl = useIntl();
  const text = intl.formatMessage({ id: item.label });
  const label = <span className={styles.label}>{text}</span>;
  return item.section === '' ? (
    <Link to="/" activeOptions={{ exact: true }} title={text}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  ) : (
    <Link to="/$section" params={{ section: item.section }} title={text}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  );
}

/** Ends this device's session and drops everything cached for the person who was signed in. */
function SignOut() {
  const intl = useIntl();
  const session = useSession();
  const client = useQueryClient();
  const text = intl.formatMessage({ id: 'auth.signOut' });
  return (
    <div className={styles.signOut}>
      <Button
        variant="ghost"
        block
        icon={LogOut}
        title={text}
        onClick={() => {
          void session.signOut().then(() => client.clear());
        }}
      >
        <span className={styles.label}>{text}</span>
      </Button>
    </div>
  );
}

/**
 * The page for the current URL, or "no access" when it belongs to a sidebar section the caller's
 * roles do not open. Paths outside the sidebar (unknown pages) fall through to the router.
 */
function Content({ roles, loading, failed, retry }: { roles: readonly string[]; loading: boolean; failed: boolean; retry: () => void }) {
  const section = useRouterState({ select: (s) => s.location.pathname.split('/')[1] ?? '' });
  if (!navItem(section) && section !== '') return <Outlet />;
  if (loading) return <Skeleton shape="block" height="160px" />;
  if (failed) return <AccessFailed retry={retry} />;
  if (!canOpen(roles, section)) {
    // Sign-in lands on `/`; a role without Pulse (a field agent) starts at its first screen instead.
    const first = section === '' ? NAV.find((n) => n.section && canOpen(roles, n.section)) : undefined;
    if (first) return <Navigate to="/$section" params={{ section: first.section }} replace />;
    return <NoAccess roles={roles} />;
  }
  return <Outlet />;
}

/**
 * Admin frame: the business app's white sidebar plus the "Admin" tag, so staff always know which
 * app they are in. Below 1024px the sidebar collapses to icons (labels stay for screen readers).
 * The sidebar lists only what the caller's roles open (D-64).
 */
export function Shell() {
  const intl = useIntl();
  const me = useMe();
  const roles = me.data?.roles ?? [];
  const items = NAV.filter((n) => canOpen(roles, n.section));
  return (
    <div className={styles.app}>
      <a className={styles.skip} href="#main">
        {intl.formatMessage({ id: 'shell.skipToContent' })}
      </a>
      <nav className={`tz-side ${styles.side}`} aria-label={intl.formatMessage({ id: 'shell.nav.label' })}>
        <div className="tz-side-brand">
          <img className={styles.mark} src="/icons/mark-96.png" alt="" width={24} height={24} />
          <span className={styles.label}>
            Today<b>Z</b>
          </span>
          <span className={`tz-side-tag ${styles.tag}`}>{intl.formatMessage({ id: 'shell.appTag' })}</span>
        </div>
        {items.map((item, i) => {
          const heading = item.group && item.group !== items[i - 1]?.group ? item.group : null;
          return (
            <Fragment key={item.section || 'pulse'}>
              {heading && (
                <div className={`tz-side-group ${styles.group}`}>
                  <span className={styles.label}>{intl.formatMessage({ id: heading })}</span>
                </div>
              )}
              <NavLink item={item} />
            </Fragment>
          );
        })}
        <SignOut />
      </nav>
      <main id="main" className={styles.main} tabIndex={-1}>
        <Content roles={roles} loading={me.isPending} failed={me.isError} retry={() => void me.refetch()} />
      </main>
    </div>
  );
}
