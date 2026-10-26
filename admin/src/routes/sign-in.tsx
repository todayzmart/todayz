import { PhoneOtpFlow, useSession, useSessionState } from '@todayz/auth';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useIntl } from 'react-intl';
import { TURNSTILE_SITE_KEY } from '../app/env';
import { usePhoneOtpLabels } from '../app/labels';
import { safeNext, validateNextSearch } from '../app/next';
import { useSecondFactor, useSecondFactorState } from '../app/second-factor';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/sign-in')({
  validateSearch: validateNextSearch,
  component: SignIn,
});

/** A-01 step 1: mobile number and SMS code. Step 2 is the authenticator code (`/two-step`). */
function SignIn() {
  const intl = useIntl();
  const session = useSession();
  const factor = useSecondFactor();
  const { done } = useSecondFactorState();
  const { status } = useSessionState();
  const router = useRouter();
  const { next } = Route.useSearch();
  const labels = usePhoneOtpLabels();
  const target = safeNext(next);

  // The phone code is in: on to the authenticator, or straight on when this tab already passed it.
  useEffect(() => {
    if (status !== 'signedIn') return;
    if (done) router.history.replace(target);
    else router.history.replace(`/two-step${target === '/' ? '' : `?next=${encodeURIComponent(target)}`}`);
  }, [status, done, router, target]);

  return (
    <>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'auth.signIn.title' })}</h1>
      <p className={styles.sub}>{intl.formatMessage({ id: 'auth.signIn.sub' })}</p>
      <PhoneOtpFlow
        className={styles.stack}
        session={session}
        siteKey={TURNSTILE_SITE_KEY}
        labels={labels}
        onSignedIn={({ nextStep }) => {
          // No second step from the server (the authenticator is off locally, D-244): straight in.
          if (nextStep) factor.setNextStep(nextStep);
          else factor.markDone();
        }}
      />
    </>
  );
}
