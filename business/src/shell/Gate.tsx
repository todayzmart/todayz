import { OfflineBanner, SessionPending, useSessionState } from '@todayz/auth';
import { Navigate, Outlet, useRouterState } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Shell } from './Shell';
import styles from './shell.module.css';

/** Routes that open without a session (B-01). Everything else needs a signed-in person. */
const OPEN = ['/sign-in'];

/**
 * The business app is for signed-in staff only. While the refresh cookie is being tried nothing
 * shows but a skeleton; signed out, any page sends the person to B-01 with `?next=` back to it.
 */
export function Gate() {
  const intl = useIntl();
  const { status } = useSessionState();
  const location = useRouterState({ select: (s) => s.location });

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
  return (
    <>
      {offline}
      <Shell />
    </>
  );
}
