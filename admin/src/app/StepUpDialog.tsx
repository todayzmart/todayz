import { useSession } from '@todayz/auth';
import { Button, Dialog, OtpInput } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { totpError, verifyTotp } from './totp';

export interface StepUpDialogProps {
  open: boolean;
  /** The authenticator code was right; retry the change that asked for it. */
  onVerified: () => void;
  onClose: () => void;
}

/**
 * The fresh authenticator code a † action asks for (`403 step_up_required`, D-64): the code
 * stamps this sign-in as checked for 10 minutes, then the screen retries the same change.
 * Wrong codes count towards the same lock as sign-in; a lock ends the session.
 */
export function StepUpDialog({ open, onVerified, onClose }: StepUpDialogProps) {
  const intl = useIntl();
  const session = useSession();
  const client = useQueryClient();
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [errorKey, setErrorKey] = useState(0);
  const [locked, setLocked] = useState(false);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);

  const close = () => {
    setCode('');
    setError(null);
    onClose();
  };

  const submit = async (value: string) => {
    if (busyRef.current || value.length !== 6) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const grant = await verifyTotp(value, 'step_up');
      session.signIn({ accessToken: grant.accessToken, expiresAt: grant.expiresAt });
      setCode('');
      setError(null);
      onVerified();
    } catch (e) {
      const err = totpError(e);
      if (err.kind === 'locked') setLocked(true);
      setError(
        err.kind === 'locked'
          ? intl.formatMessage({ id: 'auth.twoStep.locked' })
          : err.kind === 'wrong'
            ? err.attemptsLeft !== undefined
              ? intl.formatMessage({ id: 'auth.twoStep.wrongLeft' }, { count: err.attemptsLeft })
              : intl.formatMessage({ id: 'auth.twoStep.wrong' })
            : intl.formatMessage({ id: 'auth.twoStep.unavailable' }),
      );
      setErrorKey((k) => k + 1);
      setCode('');
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={close}
      title={intl.formatMessage({ id: 'common.stepUp.title' })}
      description={intl.formatMessage({ id: 'common.stepUp.body' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        locked ? (
          <Button variant="primary" onClick={() => void session.signOut().then(() => client.clear())}>
            {intl.formatMessage({ id: 'auth.twoStep.signInAgain' })}
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={close}>
              {intl.formatMessage({ id: 'common.cancel' })}
            </Button>
            <Button
              variant="primary"
              loading={busy}
              loadingLabel={intl.formatMessage({ id: 'common.saving' })}
              disabled={code.length !== 6}
              onClick={() => void submit(code)}
            >
              {intl.formatMessage({ id: 'common.stepUp.confirm' })}
            </Button>
          </>
        )
      }
    >
      <OtpInput
        label={intl.formatMessage({ id: 'auth.twoStep.codeLabel' })}
        digitLabel={(position) => intl.formatMessage({ id: 'auth.code.digit' }, { position })}
        length={6}
        value={code}
        onChange={setCode}
        onComplete={(v) => void submit(v)}
        error={error}
        errorKey={errorKey}
        disabled={busy || locked}
      />
    </Dialog>
  );
}
