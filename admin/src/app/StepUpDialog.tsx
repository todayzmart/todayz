import { ApiError } from '@todayz/api-client';
import { sendStepUp, verifyStepUp } from '@todayz/api-client/gen/auth/auth';
import { useSession } from '@todayz/auth';
import { Button, Dialog, OtpInput } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';

export interface StepUpDialogProps {
  open: boolean;
  /** The SMS code was right; retry the change that asked for it. */
  onVerified: () => void;
  onClose: () => void;
}

const CODE_LENGTH = 6;

/**
 * The fresh SMS code a † action asks for (`403 step_up_required`, D-264): the admin taps "Send
 * code", the API texts a 6-digit code to their own staff number (never one the screen names), and
 * a right code stamps this sign-in as checked for 10 minutes; then the screen retries the same
 * change. The access token does not change. The wait between codes and the hourly caps are the
 * sign-in code's (shared with it); the fifth wrong code of a request ends every admin session of
 * the person, so the dialog then only offers "Sign in again".
 *
 * The dialog stays mounted while closed, so a code already sent and the resend countdown survive
 * closing and reopening it; a used code is forgotten.
 */
export function StepUpDialog({ open, onVerified, onClose }: StepUpDialogProps) {
  const intl = useIntl();
  const session = useSession();
  const client = useQueryClient();
  const [requestId, setRequestId] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [errorKey, setErrorKey] = useState(0);
  const [wait, setWait] = useState(0);
  const [locked, setLocked] = useState(false);
  const [sending, setSending] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const busyRef = useRef(false);

  useEffect(() => {
    if (wait <= 0) return undefined;
    const t = setTimeout(() => setWait((w) => Math.max(0, w - 1)), 1000);
    return () => clearTimeout(t);
  }, [wait]);

  const fail = (message: string) => {
    setError(message);
    setErrorKey((k) => k + 1);
  };

  const close = () => {
    setCode('');
    setError(null);
    onClose();
  };

  const send = async () => {
    if (busyRef.current || wait > 0 || locked) return;
    busyRef.current = true;
    setSending(true);
    try {
      const r = await sendStepUp();
      setRequestId(r.requestId);
      setWait(r.resendAfter);
      setCode('');
      setError(null);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'rate_limited') {
        if (e.retryAfterSeconds) setWait(e.retryAfterSeconds);
        fail(intl.formatMessage({ id: 'auth.stepUp.rateLimited' }));
      } else {
        fail(intl.formatMessage({ id: 'auth.stepUp.unavailable' }));
      }
    } finally {
      busyRef.current = false;
      setSending(false);
    }
  };

  const submit = async (value: string) => {
    if (busyRef.current || !requestId || value.length !== CODE_LENGTH) return;
    busyRef.current = true;
    setVerifying(true);
    try {
      await verifyStepUp({ requestId, code: value });
      setRequestId(null);
      setCode('');
      setError(null);
      onVerified();
    } catch (e) {
      const code = e instanceof ApiError ? e.code : undefined;
      if (code === 'otp_locked') {
        setLocked(true);
        fail(intl.formatMessage({ id: 'auth.stepUp.locked' }));
      } else if (code === 'code_invalid' || code === 'validation_failed') {
        const left = e instanceof ApiError ? e.problem.extensions?.['attemptsLeft'] : undefined;
        fail(
          typeof left === 'number'
            ? intl.formatMessage({ id: 'auth.stepUp.wrongLeft' }, { count: left })
            : intl.formatMessage({ id: 'auth.stepUp.wrong' }),
        );
      } else if (code === 'otp_expired') {
        fail(intl.formatMessage({ id: 'auth.stepUp.expired' }));
      } else {
        fail(intl.formatMessage({ id: 'auth.stepUp.unavailable' }));
      }
      setCode('');
    } finally {
      busyRef.current = false;
      setVerifying(false);
    }
  };

  const sendLabel = wait > 0 ? intl.formatMessage({ id: 'auth.stepUp.resendIn' }, { seconds: wait }) : null;

  let footer;
  if (locked) {
    footer = (
      <Button variant="primary" onClick={() => void session.signOut().then(() => client.clear())}>
        {intl.formatMessage({ id: 'auth.stepUp.signInAgain' })}
      </Button>
    );
  } else if (requestId === null) {
    footer = (
      <>
        <Button variant="ghost" onClick={close}>
          {intl.formatMessage({ id: 'common.cancel' })}
        </Button>
        <Button
          variant="primary"
          loading={sending}
          loadingLabel={intl.formatMessage({ id: 'auth.stepUp.sending' })}
          disabled={wait > 0}
          onClick={() => void send()}
        >
          {sendLabel ?? intl.formatMessage({ id: 'auth.stepUp.send' })}
        </Button>
      </>
    );
  } else {
    footer = (
      <>
        <Button variant="ghost" onClick={close}>
          {intl.formatMessage({ id: 'common.cancel' })}
        </Button>
        <Button
          variant="primary"
          loading={verifying}
          loadingLabel={intl.formatMessage({ id: 'common.saving' })}
          disabled={code.length !== CODE_LENGTH}
          onClick={() => void submit(code)}
        >
          {intl.formatMessage({ id: 'common.stepUp.confirm' })}
        </Button>
      </>
    );
  }

  return (
    <Dialog
      open={open}
      onClose={close}
      title={intl.formatMessage({ id: 'common.stepUp.title' })}
      description={intl.formatMessage({ id: 'common.stepUp.body' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={footer}
    >
      {requestId === null ? (
        error && (
          <p role="alert" key={errorKey} className="tz-field-error">
            {error}
          </p>
        )
      ) : (
        <>
          <p role="status">{intl.formatMessage({ id: 'auth.stepUp.sent' })}</p>
          <OtpInput
            label={intl.formatMessage({ id: 'auth.stepUp.codeLabel' })}
            digitLabel={(position) => intl.formatMessage({ id: 'auth.code.digit' }, { position })}
            length={CODE_LENGTH}
            value={code}
            onChange={setCode}
            onComplete={(v) => void submit(v)}
            error={error}
            errorKey={errorKey}
            disabled={verifying || locked}
          />
          {!locked && (
            <Button variant="ghost" type="button" disabled={wait > 0 || sending} onClick={() => void send()}>
              {sendLabel ?? intl.formatMessage({ id: 'auth.stepUp.resend' })}
            </Button>
          )}
        </>
      )}
    </Dialog>
  );
}
