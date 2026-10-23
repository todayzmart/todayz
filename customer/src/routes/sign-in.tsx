import { PhoneOtpFlow, useSession, useSessionState } from '@todayz/auth';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { Button, TextField } from '@todayz/ui';
import { useEffect, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { TURNSTILE_SITE_KEY } from '../app/env';
import { usePhoneOtpLabels } from '../app/labels';
import { patchMe } from '../app/me';
import { safeNext, validateNextSearch } from '../app/next';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/sign-in')({
  validateSearch: validateNextSearch,
  component: SignIn,
});

/** C-10 customer sign-in: mobile number, SMS code, then (new accounts only) an optional name. */
function SignIn() {
  const intl = useIntl();
  const session = useSession();
  const { status } = useSessionState();
  const router = useRouter();
  const { next } = Route.useSearch();
  const labels = usePhoneOtpLabels();
  const [welcome, setWelcome] = useState(false);

  const go = () => router.history.push(safeNext(next));

  // Someone who is already signed in has nothing to do here.
  useEffect(() => {
    if (status === 'signedIn' && !welcome) router.history.replace(safeNext(next));
  }, [status, welcome, router, next]);

  if (welcome) return <Welcome onDone={go} />;

  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'auth.signIn.title' })}</h1>
      <p className={styles.heroSub}>{intl.formatMessage({ id: 'auth.signIn.sub' })}</p>
      <PhoneOtpFlow
        className={styles.stack}
        session={session}
        siteKey={TURNSTILE_SITE_KEY}
        labels={labels}
        onSignedIn={({ isNewUser }) => (isNewUser ? setWelcome(true) : go())}
      />
    </div>
  );
}

/** New account: tell shops what to call you. Skippable; a failure never blocks the way on. */
function Welcome({ onDone }: { onDone: () => void }) {
  const intl = useIntl();
  const session = useSession();
  const [name, setName] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(false);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (trimmed === '') return onDone();
    setSaving(true);
    try {
      await patchMe(session, { name: trimmed });
      onDone();
    } catch {
      // Nothing here is worth stopping sign-in for; say so and let them retry or skip.
      setError(true);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'auth.welcome.title' })}</h1>
      <p className={styles.heroSub}>{intl.formatMessage({ id: 'auth.welcome.sub' })}</p>
      <form className={styles.stack} onSubmit={(e) => void save(e)}>
        <TextField
          label={intl.formatMessage({ id: 'auth.welcome.name' })}
          value={name}
          maxLength={80}
          autoComplete="name"
          onChange={(e) => setName(e.target.value)}
          error={error ? intl.formatMessage({ id: 'auth.welcome.error' }) : undefined}
        />
        <Button type="submit" variant="primary" block loading={saving}>
          {intl.formatMessage({ id: 'auth.welcome.continue' })}
        </Button>
        <Button type="button" variant="ghost" block onClick={onDone}>
          {intl.formatMessage({ id: 'auth.welcome.skip' })}
        </Button>
      </form>
    </div>
  );
}
