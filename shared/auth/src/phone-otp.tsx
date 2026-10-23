// Phone and code sign-in shared by C-10, B-01 and A-01 (AU-01..AU-05): +91 number, Turnstile,
// 6-digit code with WebOTP autofill, resend timer, translated errors. Every string is a prop (the
// app passes them from its own catalogue); nothing here logs the phone, the code or a token.
import { ApiError } from '@todayz/api-client';
import { sendOtp, verifyOtp } from '@todayz/api-client/gen/auth/auth';
import { Button, OtpInput, TextField } from '@todayz/ui';
import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { getDeviceId } from './device';
import type { Session } from './session';
import { getTurnstileToken } from './turnstile';

export type OtpErrorKind = 'wrongCode' | 'expired' | 'locked' | 'rateLimited' | 'blocked' | 'network' | 'generic';

/** Maps an API failure to the message kind the screen shows. */
export function otpErrorKind(e: unknown): OtpErrorKind {
  if (e instanceof ApiError) {
    if (e.code === 'otp_expired') return 'expired';
    if (e.code === 'otp_locked') return 'locked';
    if (e.code === 'rate_limited') return 'rateLimited';
    if (e.code === 'account_blocked') return 'blocked';
    if (e.code === 'unauthorized' || e.code === 'validation_failed') return 'wrongCode';
    return 'generic';
  }
  return 'network';
}

export interface PhoneOtpLabels {
  phoneLabel: string;
  phoneHelp?: string | undefined;
  sendCode: string;
  sendingCode: string;
  codeLabel: string;
  digitLabel: (position: number) => string;
  verifying: string;
  resend: string;
  /** "Resend in 25 s" */
  resendIn: (seconds: number) => string;
  changeNumber: string;
  invalidPhone: string;
  /** Message by error kind. */
  errors: Record<OtpErrorKind | 'captcha', string>;
  /** "Code sent to +91 90000 00001" shown above the boxes. */
  sentTo: (maskedPhone: string) => string;
}

export interface SignedIn {
  userId: string;
  isNewUser: boolean;
}

export interface PhoneOtpFlowProps {
  session: Session;
  siteKey: string;
  labels: PhoneOtpLabels;
  onSignedIn: (result: SignedIn) => void;
  /** Extra fields shown on the phone step (e.g. the optional name). */
  children?: ReactNode;
  className?: string | undefined;
}

const PHONE = /^[6-9][0-9]{9}$/;
const CODE_LENGTH = 6;

function maskPhone(digits: string): string {
  return `+91 ${digits.slice(0, 2)}XXX XXX${digits.slice(-2)}`;
}

export function PhoneOtpFlow({ session, siteKey, labels, onSignedIn, children, className }: PhoneOtpFlowProps) {
  const [digits, setDigits] = useState('');
  const [phoneError, setPhoneError] = useState<string | undefined>();
  const [step, setStep] = useState<'phone' | 'code'>('phone');
  const [requestId, setRequestId] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [code, setCode] = useState('');
  const [codeError, setCodeError] = useState<string | undefined>();
  const [errorKey, setErrorKey] = useState(0);
  const [verifying, setVerifying] = useState(false);
  const [wait, setWait] = useState(0);
  const verifyingRef = useRef(false);

  useEffect(() => {
    if (wait <= 0) return undefined;
    const t = setTimeout(() => setWait((w) => Math.max(0, w - 1)), 1000);
    return () => clearTimeout(t);
  }, [wait]);

  const send = useCallback(async () => {
    if (!PHONE.test(digits)) {
      setPhoneError(labels.invalidPhone);
      return;
    }
    setPhoneError(undefined);
    setSending(true);
    try {
      let token: string;
      try {
        token = await getTurnstileToken(siteKey);
      } catch {
        setPhoneError(labels.errors.captcha);
        return;
      }
      const r = await sendOtp({ phone: `+91${digits}`, purpose: 'login', app: session.app, turnstileToken: token });
      setRequestId(r.requestId);
      setWait(r.resendAfter);
      setCode('');
      setCodeError(undefined);
      setStep('code');
    } catch (e) {
      const kind = otpErrorKind(e);
      setPhoneError(labels.errors[kind === 'wrongCode' ? 'generic' : kind]);
      if (e instanceof ApiError && e.retryAfterSeconds) setWait(e.retryAfterSeconds);
    } finally {
      setSending(false);
    }
  }, [digits, labels, session.app, siteKey]);

  const verify = useCallback(
    async (value: string) => {
      if (!requestId || verifyingRef.current) return;
      verifyingRef.current = true;
      setVerifying(true);
      setCodeError(undefined);
      try {
        const r = await verifyOtp({
          requestId,
          code: value,
          app: session.app,
          deviceId: getDeviceId(session.app),
        });
        session.signIn({ accessToken: r.accessToken, expiresAt: r.expiresAt, userId: r.userId });
        onSignedIn({ userId: r.userId, isNewUser: r.isNewUser });
      } catch (e) {
        setCodeError(labels.errors[otpErrorKind(e)]);
        setErrorKey((k) => k + 1);
        setCode('');
      } finally {
        verifyingRef.current = false;
        setVerifying(false);
      }
    },
    [labels, onSignedIn, requestId, session],
  );

  // WebOTP: Chrome on Android hands the SMS code to the page (AU-05). Silently absent elsewhere.
  useEffect(() => {
    if (step !== 'code') return undefined;
    const creds = navigator.credentials as
      | (CredentialsContainer & { get(o: unknown): Promise<{ code?: string } | null> })
      | undefined;
    if (!creds || !('OTPCredential' in window)) return undefined;
    const ac = new AbortController();
    creds
      .get({ otp: { transport: ['sms'] }, signal: ac.signal })
      .then((c) => {
        if (c?.code && /^[0-9]{6}$/.test(c.code)) {
          setCode(c.code);
          void verify(c.code);
        }
      })
      .catch(() => undefined);
    return () => ac.abort();
  }, [step, verify]);

  const onPhoneSubmit = (e: FormEvent) => {
    e.preventDefault();
    void send();
  };

  if (step === 'code') {
    return (
      <div className={className}>
        <p>{labels.sentTo(maskPhone(digits))}</p>
        <OtpInput
          label={labels.codeLabel}
          digitLabel={labels.digitLabel}
          length={CODE_LENGTH}
          value={code}
          onChange={setCode}
          onComplete={(c) => void verify(c)}
          error={codeError}
          errorKey={errorKey}
          disabled={verifying}
          // The person just pressed "send code": the code step is the only thing to do next.
          // eslint-disable-next-line jsx-a11y/no-autofocus
          autoFocus
        />
        {verifying && <p role="status">{labels.verifying}</p>}
        <div className="tz-stack-row">
          <Button
            variant="ghost"
            disabled={wait > 0 || sending}
            onClick={() => void send()}
            type="button"
          >
            {wait > 0 ? labels.resendIn(wait) : labels.resend}
          </Button>
          <Button
            variant="ghost"
            type="button"
            onClick={() => {
              setStep('phone');
              setCode('');
              setCodeError(undefined);
            }}
          >
            {labels.changeNumber}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form className={className} onSubmit={onPhoneSubmit} noValidate>
      <TextField
        label={labels.phoneLabel}
        prefix="+91"
        type="tel"
        inputMode="numeric"
        autoComplete="tel-national"
        maxLength={10}
        value={digits}
        onChange={(e) => setDigits(e.target.value.replace(/\D/g, '').slice(0, 10))}
        error={phoneError}
        help={labels.phoneHelp}
      />
      {children}
      <Button type="submit" variant="primary" block loading={sending} loadingLabel={labels.sendingCode} disabled={wait > 0 && !sending}>
        {wait > 0 ? labels.resendIn(wait) : labels.sendCode}
      </Button>
    </form>
  );
}
