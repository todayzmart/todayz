import { PhoneOtpFlow, useSession, useSessionState } from '@todayz/auth';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useIntl } from 'react-intl';
import { TURNSTILE_SITE_KEY } from '../app/env';
import { usePhoneOtpLabels } from '../app/labels';
import { safeNext, validateNextSearch } from '../app/next';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/sign-in')({
  validateSearch: validateNextSearch,
  component: SignIn,
});

/** A-01: mobile number and SMS code; the code alone signs an admin in (D-264). */
function SignIn() {
  const intl = useIntl();
  const session = useSession();
  const { status } = useSessionState();
  const router = useRouter();
  const { next } = Route.useSearch();
  const labels = usePhoneOtpLabels();
  const target = safeNext(next);

  // Signed in (the code just now, or a cookie that was still good): on to where they were going.
  useEffect(() => {
    if (status === 'signedIn') router.history.replace(target);
  }, [status, router, target]);

  return (
    <>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'auth.signIn.title' })}</h1>
      <p className={styles.sub}>{intl.formatMessage({ id: 'auth.signIn.sub' })}</p>
      <PhoneOtpFlow
        className={styles.stack}
        session={session}
        siteKey={TURNSTILE_SITE_KEY}
        labels={labels}
        onSignedIn={() => undefined}
      />
    </>
  );
}
