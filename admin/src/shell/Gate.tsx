import { OfflineBanner, SessionPending, useSessionState } from '@todayz/auth';
import { Navigate, Outlet, useRouterState } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Shell } from './Shell';
import styles from './shell.module.css';

/** Routes that open without a session (A-01). */
const OPEN = ['/sign-in'];

/**
 * The admin console is for signed-in staff: the SMS code alone signs an admin in (D-264), and a
 * † action asks for a fresh SMS code of its own (StepUpDialog). While the refresh cookie is being
 * tried nothing shows but a skeleton; signed out, any page sends the person to A-01 with `?next=`
 * back to it.
 */
export function Gate() {
  const intl = useIntl();
  const { status } = useSessionState();
  const location = useRouterState({ select: (s) => s.location });
  // The page the Outlet holds. Right after a redirect to /sign-in, `location` is already /sign-in
  // while the Outlet still holds the old match; the bare layout renders nothing until they agree, so
  // a staff page never mounts signed out (and never fires its API calls).
  const rendered = useRouterState({ select: (s) => s.matches[s.matches.length - 1]?.pathname });

  const offline = (
    <OfflineBanner
      className={styles.offline}
      title={intl.formatMessage({ id: 'app.offline.title' })}
      message={intl.formatMessage({ id: 'app.offline.body' })}
    />
  );
  const bare =
    rendered === location.pathname ? (
      <>
        {offline}
        <main id="main" className={styles.signIn} tabIndex={-1}>
          <Outlet />
        </main>
      </>
    ) : null;
  const here = location.pathname + location.searchStr;
  const next = here === '/' || OPEN.includes(location.pathname) ? {} : { next: here };

  if (OPEN.includes(location.pathname)) return bare;
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
  if (status === 'signedOut') return <Navigate to="/sign-in" search={next} replace />;
  return (
    <>
      {offline}
      <Shell />
    </>
  );
}
