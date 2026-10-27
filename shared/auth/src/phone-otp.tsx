// Phone and code sign-in shared by C-10, B-01 and A-01 (AU-01..AU-05): +91 number, Turnstile,
// 6-digit code with WebOTP autofill, resend timer, translated errors. Every string is a prop (the
// app passes them from its own catalogue); nothing here logs the phone, the code or a token.
// D-275: GET /v1/auth/config says who sends the code. `sms`: TodayZ (Turnstile, otp/send,
// otp/verify). `firebase`: Firebase Auth in the page (reCAPTCHA, no WebOTP), then the Firebase ID
// token is swapped for a TodayZ session (auth/firebase/verify).
import { ApiError } from '@todayz/api-client';
import { getAuthConfig, sendOtp, verifyFirebaseSignIn, verifyOtp } from '@todayz/api-client/gen/auth/auth';
import type { AuthConfigResponse } from '@todayz/api-client/gen/model/authConfigResponse';
import { Button, OtpInput, TextField } from '@todayz/ui';
import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { getDeviceId } from './device';
import { FirebasePhoneError, loadFirebasePhone, type FirebaseConfirmation } from './firebase';
import type { Session } from './session';
import { getTurnstileToken } from './turnstile';

export type OtpErrorKind =
  | 'wrongCode'
  | 'expired'
  | 'locked'
  | 'signinLocked'
  | 'rateLimited'
  | 'blocked'
  | 'methodChanged'
  | 'network'
  | 'generic';

/** Maps an API failure to the message kind the screen shows. */
export function otpErrorKind(e: unknown): OtpErrorKind {
  if (e instanceof ApiError) {
    if (e.code === 'otp_expired') return 'expired';
    if (e.code === 'otp_locked') return 'locked';
    // D-264: the admin app's sign-in is locked for this number after too many wrong codes.
    if (e.code === 'signin_locked') return 'signinLocked';
    if (e.code === 'rate_limited') return 'rateLimited';
    if (e.code === 'account_blocked') return 'blocked';
    // D-275: the deploy moved between SMS and Firebase while this page was open.
    if (e.code === 'sign_in_method_changed') return 'methodChanged';
    if (e.code === 'unauthorized' || e.code === 'validation_failed') return 'wrongCode';
    return 'generic';
  }
  return 'network';
}

/** The message for an error kind, with the fallbacks the labels allow. */
export function otpErrorMessage(errors: PhoneOtpLabels['errors'], kind: OtpErrorKind): string {
  if (kind === 'signinLocked') return errors.signinLocked ?? errors.locked;
  if (kind === 'methodChanged') return errors.methodChanged ?? errors.generic;
  return errors[kind];
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
  /**
   * Message by error kind. `signinLocked` (admin app only, D-264) falls back to `locked`, and
   * `methodChanged` (D-275) to `generic`, when an app does not set them.
   */
  errors: Record<Exclude<OtpErrorKind, 'signinLocked' | 'methodChanged'> | 'captcha', string> & {
    signinLocked?: string;
    methodChanged?: string;
  };
  /** "Code sent to +91 90000 00001" shown above the boxes. */
  sentTo: (maskedPhone: string) => string;
  /** Firebase path only (D-275): the line saying reCAPTCHA and Google's terms apply. */
  recaptchaNotice?: string | undefined;
  /** The app's locale (en, hi, kn, te, ta): Firebase sends its SMS in it where it can. */
  languageCode?: string | undefined;
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
// Firebase enforces its own send limits (D-275); this only paces the resend button.
const FIREBASE_RESEND_AFTER = 30;

type Provider = 'sms' | 'firebase';

/** The provider and, for firebase, its web config. Any failure reads as sms: the API refuses with 409 if wrong. */
async function readAuthConfig(): Promise<AuthConfigResponse> {
  try {
    const c = await getAuthConfig();
    if (c.otpProvider === 'firebase' && c.firebase) return c;
  } catch {
    // The SMS path, which still works or explains itself.
  }
  return { otpProvider: 'sms' };
}

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
  const [provider, setProvider] = useState<Provider>('sms');
  const verifyingRef = useRef(false);
  const configRef = useRef<Promise<AuthConfigResponse> | null>(null);
  const confirmationRef = useRef<FirebaseConfirmation | null>(null);
  // The latest callback, read when a code is verified, so a new function each render does not re-run effects.
  const onSignedInRef = useRef(onSignedIn);
  const labelsRef = useRef(labels);
  useEffect(() => {
    onSignedInRef.current = onSignedIn;
    labelsRef.current = labels;
  });

  const refreshConfig = useCallback(() => {
    const p = readAuthConfig();
    configRef.current = p;
    void p.then((c) => {
      if (configRef.current !== p) return;
      setProvider(c.otpProvider);
      // Warm the SDK chunk while the number is typed.
      if (c.otpProvider === 'firebase' && c.firebase) void loadFirebasePhone(c.firebase).catch(() => undefined);
    });
    return p;
  }, []);

  useEffect(() => {
    void refreshConfig();
  }, [refreshConfig]);

  // The deploy switched provider under this page: read the config again and start from the number.
  const methodChanged = useCallback(() => {
    void refreshConfig();
    setStep('phone');
    setRequestId(null);
    confirmationRef.current = null;
    setCode('');
    setCodeError(undefined);
    setWait(0);
    setPhoneError(otpErrorMessage(labelsRef.current.errors, 'methodChanged'));
  }, [refreshConfig]);

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
    // Resending happens on the code step: its errors show there, not on the hidden phone field.
    const fail = (message: string) => {
      if (step === 'code') {
        setCodeError(message);
        setErrorKey((k) => k + 1);
      } else {
        setPhoneError(message);
      }
    };
    try {
      const config = await (configRef.current ?? refreshConfig());
      if (config.otpProvider === 'firebase' && config.firebase) {
        try {
          const fb = await loadFirebasePhone(config.firebase);
          confirmationRef.current = await fb.send(`+91${digits}`, labels.languageCode);
        } catch (e) {
          const kind = e instanceof FirebasePhoneError ? e.kind : 'generic';
          if (kind === 'invalidPhone') fail(labels.invalidPhone);
          else if (kind === 'captcha') fail(labels.errors.captcha);
          else fail(otpErrorMessage(labels.errors, kind === 'wrongCode' ? 'generic' : kind));
          return;
        }
        setRequestId(null);
        setWait(FIREBASE_RESEND_AFTER);
        setCode('');
        setCodeError(undefined);
        setStep('code');
        return;
      }
      let token: string;
      try {
        token = await getTurnstileToken(siteKey);
      } catch {
        fail(labels.errors.captcha);
        return;
      }
      const r = await sendOtp({ phone: `+91${digits}`, purpose: 'login', app: session.app, turnstileToken: token });
      confirmationRef.current = null;
      setRequestId(r.requestId);
      setWait(r.resendAfter);
      setCode('');
      setCodeError(undefined);
      setStep('code');
    } catch (e) {
      const kind = otpErrorKind(e);
      if (kind === 'methodChanged') {
        methodChanged();
        return;
      }
      fail(otpErrorMessage(labels.errors, kind === 'wrongCode' ? 'generic' : kind));
      // A sign-in lock runs for hours and another super admin may lift it sooner: show the
      // message, not an hours-long countdown that would also hold every other number.
      if (kind !== 'signinLocked' && e instanceof ApiError && e.retryAfterSeconds) setWait(e.retryAfterSeconds);
    } finally {
      setSending(false);
    }
  }, [digits, labels, methodChanged, refreshConfig, session.app, siteKey, step]);

  const verify = useCallback(
    async (value: string) => {
      const confirmation = confirmationRef.current;
      if ((!requestId && !confirmation) || verifyingRef.current) return;
      verifyingRef.current = true;
      setVerifying(true);
      setCodeError(undefined);
      try {
        const deviceId = getDeviceId(session.app);
        const r = confirmation
          ? await verifyFirebaseSignIn({ idToken: await confirmation.confirm(value), app: session.app, deviceId })
          : await verifyOtp({ requestId: requestId ?? '', code: value, app: session.app, deviceId });
        session.signIn({ accessToken: r.accessToken, expiresAt: r.expiresAt, userId: r.userId });
        onSignedInRef.current({ userId: r.userId, isNewUser: r.isNewUser });
      } catch (e) {
        const errors = labelsRef.current.errors;
        if (e instanceof FirebasePhoneError) {
          setCodeError(
            e.kind === 'captcha' || e.kind === 'invalidPhone' ? errors.generic : otpErrorMessage(errors, e.kind),
          );
        } else {
          const kind = otpErrorKind(e);
          if (kind === 'methodChanged') {
            methodChanged();
            return;
          }
          setCodeError(otpErrorMessage(errors, kind));
        }
        setErrorKey((k) => k + 1);
        setCode('');
      } finally {
        verifyingRef.current = false;
        setVerifying(false);
      }
    },
    [methodChanged, requestId, session],
  );

  // WebOTP: Chrome on Android hands the SMS code to the page (AU-05). Silently absent elsewhere,
  // and on the Firebase path, whose SMS text is Google's and carries no origin line (D-275).
  useEffect(() => {
    if (step !== 'code' || provider !== 'sms') return undefined;
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
  }, [provider, step, verify]);

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
      {provider === 'firebase' && labels.recaptchaNotice && <p className="tz-field-help">{labels.recaptchaNotice}</p>}
    </form>
  );
}
