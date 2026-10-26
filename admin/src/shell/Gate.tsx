import { OfflineBanner, SessionPending, useSessionState } from '@todayz/auth';
import { Navigate, Outlet, useRouterState } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useIntl } from 'react-intl';
import { ADMIN_TOTP_OFF } from '../app/env';
import { useSecondFactor, useSecondFactorState } from '../app/second-factor';
import { Shell } from './Shell';
import styles from './shell.module.css';

/** Routes that open without a session (A-01 step 1). */
const OPEN = ['/sign-in'];
/** A-01 step 2: signed in with the phone code, authenticator still to do. */
const SECOND_STEP = '/two-step';

/**
 * The admin console is for signed-in staff who have also passed the authenticator step. While the
 * refresh cookie is being tried nothing shows but a skeleton; signed out, any page sends the person
 * to A-01 with `?next=` back to it; signed in without the second factor, to the two-step check.
 */
export function Gate() {
  const intl = useIntl();
  const { status } = useSessionState();
  const factor = useSecondFactor();
  const done = useSecondFactorState().done || ADMIN_TOTP_OFF;
  const location = useRouterState({ select: (s) => s.location });
  // The page the Outlet holds. Right after a redirect to /sign-in, `location` is already /sign-in
  // while the Outlet still holds the old match; the bare layout renders nothing until they agree, so
  // a staff page never mounts signed out (and never fires its API calls).
  const rendered = useRouterState({ select: (s) => s.matches[s.matches.length - 1]?.pathname });

  // A signed-out browser keeps nothing about the authenticator step.
  useEffect(() => {
    if (status === 'signedOut') factor.clear();
  }, [status, factor]);

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
  const next = here === '/' || OPEN.includes(location.pathname) || location.pathname === SECOND_STEP ? {} : { next: here };

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
  if (!done) {
    if (location.pathname === SECOND_STEP) return bare;
    return <Navigate to="/two-step" search={next} replace />;
  }
  if (location.pathname === SECOND_STEP) return <Navigate to="/" replace />;
  return (
    <>
      {offline}
      <Shell />
    </>
  );
}
