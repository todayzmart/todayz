import { useSession } from '@todayz/auth';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { ApiError } from '@todayz/api-client';
import { Button, OtpInput, Skeleton, TextField } from '@todayz/ui';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { safeNext, validateNextSearch } from '../app/next';
import { useSecondFactor, useSecondFactorState } from '../app/second-factor';
import { enrolTotp, groupSecret, recoverTotp, totpError, verifyTotp } from '../app/totp';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/two-step')({
  validateSearch: validateNextSearch,
  component: TwoStep,
});

type Mode = 'enrol' | 'verify' | 'recover';

/**
 * A-01 step 2: the authenticator code. First sign-in enrols (setup key, then the first code, then the
 * 10 recovery codes once); later sign-ins verify; a lost phone uses a recovery code.
 * The setup key is shown as text and a link because the QR code needs a library this repo does not
 * have yet.
 */
function TwoStep() {
  const intl = useIntl();
  const session = useSession();
  const factor = useSecondFactor();
  const client = useQueryClient();
  const router = useRouter();
  const { next } = Route.useSearch();
  const { nextStep } = useSecondFactorState();

  const [chosen, setMode] = useState<Mode>(nextStep === 'totp_enrol' ? 'enrol' : 'verify');
  // Held in this component's state only: shown once, never stored, never logged.
  const [codes, setCodes] = useState<string[] | null>(null);
  const [left, setLeft] = useState<number | null>(null);
  const [saved, setSaved] = useState(false);
  const [locked, setLocked] = useState(false);
  const [code, setCode] = useState('');
  const [recovery, setRecovery] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [errorKey, setErrorKey] = useState(0);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);

  const enrol = useQuery({
    queryKey: ['totp-enrol'],
    queryFn: enrolTotp,
    enabled: chosen === 'enrol' && codes === null,
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  });

  // Already enrolled (a reload after enrolling, or a second device): ask for the code instead.
  const alreadyEnrolled = enrol.error instanceof ApiError && enrol.error.code === 'conflict';
  const mode: Mode = chosen === 'enrol' && alreadyEnrolled ? 'verify' : chosen;

  const finish = () => {
    router.history.replace(safeNext(next));
    factor.markDone();
  };

  const fail = (e: unknown, wrong: string) => {
    const r = totpError(e);
    if (r.kind === 'locked') {
      setLocked(true);
      return;
    }
    if (r.kind === 'wrong') {
      setError(
        r.attemptsLeft !== undefined
          ? intl.formatMessage({ id: 'auth.twoStep.wrongLeft' }, { count: r.attemptsLeft })
          : wrong,
      );
    } else {
      setError(intl.formatMessage({ id: 'auth.twoStep.unavailable' }));
    }
    setErrorKey((k) => k + 1);
  };

  const run = async (work: () => Promise<void>, wrong: string) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await work();
    } catch (e) {
      fail(e, wrong);
      setCode('');
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const submitCode = (value: string) =>
    run(async () => {
      const grant = await verifyTotp(value, 'login');
      session.signIn({ accessToken: grant.accessToken, expiresAt: grant.expiresAt });
      if (mode === 'enrol' && enrol.data) {
        setCodes(enrol.data.recoveryCodes);
        client.removeQueries({ queryKey: ['totp-enrol'] });
      } else {
        finish();
      }
    }, intl.formatMessage({ id: 'auth.twoStep.wrong' }));

  const submitRecovery = () =>
    run(async () => {
      const grant = await recoverTotp(recovery);
      session.signIn({ accessToken: grant.accessToken, expiresAt: grant.expiresAt });
      setRecovery('');
      setLeft(grant.recoveryCodesLeft);
    }, intl.formatMessage({ id: 'auth.recovery.wrong' }));

  const title = (
    <h1 className="tz-page-title">{intl.formatMessage({ id: 'auth.twoStep.title' })}</h1>
  );

  if (locked) {
    return (
      <>
        {title}
        <div role="alert" className="tz-banner tz-banner-warning">
          {intl.formatMessage({ id: 'auth.twoStep.locked' })}
        </div>
        <Button
          variant="primary"
          onClick={() => {
            void session.signOut().then(() => client.clear());
          }}
        >
          {intl.formatMessage({ id: 'auth.twoStep.signInAgain' })}
        </Button>
      </>
    );
  }

  if (codes !== null) {
    return (
      <section className={styles.stack} aria-labelledby="recovery-title">
        <h1 id="recovery-title" className="tz-page-title">
          {intl.formatMessage({ id: 'auth.recovery.shownTitle' })}
        </h1>
        <p className={styles.sub}>{intl.formatMessage({ id: 'auth.recovery.shownBody' })}</p>
        <ul className={styles.codes} aria-label={intl.formatMessage({ id: 'auth.recovery.listLabel' })}>
          {codes.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        <label>
          <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />{' '}
          {intl.formatMessage({ id: 'auth.recovery.saved' })}
        </label>
        <Button
          variant="primary"
          disabled={!saved}
          onClick={() => {
            setCodes(null);
            finish();
          }}
        >
          {intl.formatMessage({ id: 'auth.recovery.continue' })}
        </Button>
      </section>
    );
  }

  if (left !== null) {
    return (
      <section className={styles.stack}>
        {title}
        <p className={styles.sub} role="status">
          {intl.formatMessage({ id: 'auth.recovery.left' }, { count: left })}
        </p>
        <Button variant="primary" onClick={finish}>
          {intl.formatMessage({ id: 'auth.recovery.continueRecover' })}
        </Button>
      </section>
    );
  }

  if (mode === 'recover') {
    return (
      <form
        className={styles.stack}
        onSubmit={(e) => {
          e.preventDefault();
          if (recovery.trim() !== '') void submitRecovery();
        }}
      >
        {title}
        <TextField
          label={intl.formatMessage({ id: 'auth.recovery.label' })}
          help={intl.formatMessage({ id: 'auth.recovery.help' })}
          error={error}
          value={recovery}
          onChange={(e) => setRecovery(e.target.value)}
          autoComplete="off"
          autoCapitalize="characters"
          spellCheck={false}
          maxLength={16}
        />
        <Button type="submit" variant="primary" loading={busy}>
          {intl.formatMessage({ id: 'auth.recovery.submit' })}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setError(undefined);
            setMode('verify');
          }}
        >
          {intl.formatMessage({ id: 'auth.twoStep.useApp' })}
        </Button>
      </form>
    );
  }

  if (mode === 'enrol' && !enrol.data) {
    if (enrol.isError) {
      return (
        <section className={styles.stack}>
          {title}
          <div role="alert" className="tz-banner tz-banner-warning">
            {intl.formatMessage({ id: 'auth.twoStep.unavailable' })}
          </div>
          <Button variant="primary" onClick={() => void enrol.refetch()}>
            {intl.formatMessage({ id: 'auth.twoStep.retry' })}
          </Button>
        </section>
      );
    }
    return (
      <div className={styles.stack} role="status" aria-label={intl.formatMessage({ id: 'auth.twoStep.loading' })}>
        {title}
        <Skeleton height="6rem" />
      </div>
    );
  }

  return (
    <div className={styles.stack}>
      {title}
      <p className={styles.sub}>
        {intl.formatMessage({ id: mode === 'enrol' ? 'auth.twoStep.enrolSub' : 'auth.twoStep.verifySub' })}
      </p>
      {mode === 'enrol' && enrol.data && (
        <>
          <div>
            <p className="tz-field-label">{intl.formatMessage({ id: 'auth.twoStep.secretLabel' })}</p>
            <p className={styles.secret} data-testid="totp-secret">
              {groupSecret(enrol.data.secret)}
            </p>
            <p className={styles.sub}>{intl.formatMessage({ id: 'auth.twoStep.secretHelp' })}</p>
          </div>
          {/* The address comes from the API, but only an otpauth:// link is ever made clickable. */}
          {enrol.data.otpauthUri.startsWith('otpauth://') && (
            <a className="tz-btn" href={enrol.data.otpauthUri}>
              {intl.formatMessage({ id: 'auth.twoStep.uriOpen' })}
            </a>
          )}
        </>
      )}
      <OtpInput
        label={intl.formatMessage({ id: 'auth.twoStep.codeLabel' })}
        digitLabel={(position) => intl.formatMessage({ id: 'auth.code.digit' }, { position })}
        length={6}
        value={code}
        onChange={setCode}
        onComplete={(v) => void submitCode(v)}
        error={error}
        errorKey={errorKey}
        disabled={busy}
      />
      <Button variant="primary" loading={busy} disabled={code.length !== 6} onClick={() => void submitCode(code)}>
        {intl.formatMessage({ id: 'auth.twoStep.confirm' })}
      </Button>
      {mode === 'verify' && (
        <Button
          variant="ghost"
          onClick={() => {
            setError(undefined);
            setMode('recover');
          }}
        >
          {intl.formatMessage({ id: 'auth.twoStep.useRecovery' })}
        </Button>
      )}
    </div>
  );
}
