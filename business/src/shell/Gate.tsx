import { OfflineBanner, SessionPending, useSessionState } from '@todayz/auth';
import { Navigate, Outlet, useRouterState } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { useBusinesses } from '../stores/queries';
import { Shell } from './Shell';
import styles from './shell.module.css';

/** Routes that open without a session (B-01). Everything else needs a signed-in person. */
const OPEN = ['/sign-in'];

/** Signed-in routes that draw their own frame instead of the shell: B-02 has its own green step rail (D-274). */
const BARE = ['/onboarding'];

/** Signed-in routes a person with no shop may still open: an invitation link makes them staff, not an owner. */
const NO_SHOP_OK = (path: string) => BARE.includes(path) || path.startsWith('/invite/');

/**
 * The business app is for signed-in staff only. While the refresh cookie is being tried nothing
 * shows but a skeleton; signed out, any page sends the person to B-01 with `?next=` back to it.
 */
export function Gate() {
  const intl = useIntl();
  const { status } = useSessionState();
  const location = useRouterState({ select: (s) => s.location });
  const businesses = useBusinesses();

  const offline = (
    <OfflineBanner
      className={styles.offline}
      title={intl.formatMessage({ id: 'app.offline.title' })}
      message={intl.formatMessage({ id: 'app.offline.body' })}
    />
  );

  if (OPEN.includes(location.pathname)) {
    return (
      <>
        {offline}
        <main id="main" className={styles.signIn} tabIndex={-1}>
          <Outlet />
        </main>
      </>
    );
  }
  if (status === 'unknown') {
    return (
      <SessionPending
        className={styles.signIn}
        loadingLabel={intl.formatMessage({ id: 'app.loading' })}
        offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
        offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
        retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
      />
    );
  }
  if (status === 'signedOut') {
    const here = location.pathname + location.searchStr;
    return <Navigate to="/sign-in" search={here === '/' ? {} : { next: here }} replace />;
  }
  // No business and no invitation: a new signup goes straight to B-02 instead of an empty dashboard.
  // Only a known-empty answer redirects; while the list loads or fails (offline) the shell shows as before.
  const noShop = businesses.data && businesses.data.items.length === 0 && businesses.data.invitations.length === 0;
  if (noShop && !NO_SHOP_OK(location.pathname)) {
    return <Navigate to="/onboarding" replace />;
  }
  return (
    <>
      {offline}
      {BARE.includes(location.pathname) ? <Outlet /> : <Shell />}
    </>
  );
}
