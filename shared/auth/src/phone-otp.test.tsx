import { configure } from '@todayz/api-client';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PhoneOtpFlow, otpErrorMessage, type PhoneOtpLabels } from './phone-otp';
import type * as FirebaseModule from './firebase';
import type { Session } from './session';
import { TURNSTILE_TEST_SITE_KEY } from './turnstile';

const firebase = vi.hoisted(() => ({ send: vi.fn(), confirm: vi.fn(), load: vi.fn() }));
vi.mock('./firebase', async (original) => ({
  ...(await original<typeof FirebaseModule>()),
  loadFirebasePhone: firebase.load,
}));

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}

function labels(): PhoneOtpLabels {
  return {
    phoneLabel: 'Mobile number',
    sendCode: 'Send code',
    sendingCode: 'Sending',
    codeLabel: 'Enter the 6-digit code',
    digitLabel: (n) => `Digit ${n}`,
    verifying: 'Checking',
    resend: 'Resend',
    resendIn: (s) => `Resend in ${s} s`,
    changeNumber: 'Change number',
    invalidPhone: 'Invalid',
    errors: { generic: 'g', captcha: 'c', wrongCode: 'w', expired: 'e', locked: 'l', rateLimited: 'r', blocked: 'b', network: 'n' },
    sentTo: (p) => `Sent to ${p}`,
  };
}

describe('PhoneOtpFlow WebOTP', () => {
  let get: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    get = vi.fn(() => new Promise(() => undefined));
    Object.defineProperty(window, 'OTPCredential', { value: function OTPCredential() {}, configurable: true });
    Object.defineProperty(navigator, 'credentials', { value: { get }, configurable: true });
    configure({
      baseUrl: 'https://api.test',
      fetch: (async () =>
        new Response(JSON.stringify({ requestId: 'req-1', resendAfter: 30 }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        })) as typeof fetch,
    });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window, 'OTPCredential');
    configure({});
  });

  it('asks the browser for the code once, however often the parent renders with new callbacks', async () => {
    const session = { app: 'customer', signIn: vi.fn() } as unknown as Session;
    const ui = (onSignedIn: () => void) => (
      <PhoneOtpFlow session={session} siteKey={TURNSTILE_TEST_SITE_KEY} labels={labels()} onSignedIn={onSignedIn} />
    );
    const view = render(ui(() => undefined));
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/Sent to/);
    // The WebOTP request starts in an effect after the code screen shows; under load it lands a tick later.
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    view.rerender(ui(() => undefined));
    view.rerender(ui(() => undefined));
    expect(get).toHaveBeenCalledTimes(1);
  });
});

describe('PhoneOtpFlow sign-in lock (D-264)', () => {
  afterEach(() => configure({}));

  function problem(code: string, status: number, headers: Record<string, string> = {}) {
    return new Response(JSON.stringify({ type: 'about:blank', code, status, title: code }), {
      status,
      headers: { 'content-type': 'application/problem+json', ...headers },
    });
  }

  it('maps signin_locked to its own kind, falling back to the locked message', () => {
    const l = labels();
    expect(otpErrorMessage(l.errors, 'signinLocked')).toBe('l');
    expect(otpErrorMessage({ ...l.errors, signinLocked: 'sl' }, 'signinLocked')).toBe('sl');
    expect(otpErrorMessage(l.errors, 'rateLimited')).toBe('r');
  });

  it('shows the lock on send without an hours-long countdown', async () => {
    configure({
      baseUrl: 'https://api.test',
      fetch: (async () => problem('signin_locked', 429, { 'retry-after': '86400' })) as typeof fetch,
    });
    const session = { app: 'admin', signIn: vi.fn() } as unknown as Session;
    render(
      <PhoneOtpFlow
        session={session}
        siteKey={TURNSTILE_TEST_SITE_KEY}
        labels={{ ...labels(), errors: { ...labels().errors, signinLocked: 'Sign-in locked' } }}
        onSignedIn={() => undefined}
      />,
    );
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText('Sign-in locked');
    expect(screen.getByRole('button', { name: 'Send code' })).toBeEnabled();
    expect(screen.queryByText(/Resend in/)).toBeNull();
  });

  it('shows the lock when the tenth wrong code trips it on verify', async () => {
    let calls = 0;
    configure({
      baseUrl: 'https://api.test',
      fetch: (async (input: RequestInfo | URL) => {
        if (String(input).endsWith('/v1/auth/config')) return json({ otpProvider: 'sms' });
        calls += 1;
        if (calls === 1) {
          return new Response(JSON.stringify({ requestId: 'req-1', resendAfter: 30 }), {
            status: 200,
            headers: { 'content-type': 'application/json' },
          });
        }
        return problem('signin_locked', 429, { 'retry-after': '86400' });
      }) as typeof fetch,
    });
    const session = { app: 'admin', signIn: vi.fn() } as unknown as Session;
    render(
      <PhoneOtpFlow
        session={session}
        siteKey={TURNSTILE_TEST_SITE_KEY}
        labels={{ ...labels(), errors: { ...labels().errors, signinLocked: 'Sign-in locked' } }}
        onSignedIn={() => undefined}
      />,
    );
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/Sent to/);
    // Autofill: the whole code lands in the first box.
    const first = screen.getByRole('group', { name: 'Enter the 6-digit code' }).querySelector('input') as HTMLInputElement;
    fireEvent.change(first, { target: { value: '123456' } });
    await waitFor(() => expect(calls).toBe(2));
    await screen.findByText('Sign-in locked');
    expect(session.signIn).not.toHaveBeenCalled();
  });
});

describe('PhoneOtpFlow on the Firebase path (D-275)', () => {
  const config = {
    otpProvider: 'firebase',
    firebase: { apiKey: 'k', authDomain: 'd', projectId: 'p', appId: 'a' },
  };
  let get: ReturnType<typeof vi.fn>;
  let calls: string[];
  let verifyStatus: number;
  let verifyBody: unknown;

  beforeEach(async () => {
    const { FirebasePhoneError } = await import('./firebase');
    firebase.send.mockReset();
    firebase.confirm.mockReset();
    firebase.load.mockReset();
    firebase.load.mockResolvedValue({ send: firebase.send });
    firebase.send.mockResolvedValue({ confirm: firebase.confirm });
    firebase.confirm.mockImplementation(async (code: string) => {
      if (code !== '000000') throw new FirebasePhoneError('wrongCode');
      return 'id-token';
    });
    get = vi.fn(() => new Promise(() => undefined));
    Object.defineProperty(window, 'OTPCredential', { value: function OTPCredential() {}, configurable: true });
    Object.defineProperty(navigator, 'credentials', { value: { get }, configurable: true });
    calls = [];
    verifyStatus = 200;
    verifyBody = { accessToken: 'at', expiresAt: '2026-10-27T10:15:00Z', userId: 'u1', isNewUser: true };
    configure({
      baseUrl: 'https://api.test',
      fetch: (async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        calls.push(url.replace('https://api.test', ''));
        if (url.endsWith('/v1/auth/config')) return json(config);
        if (url.endsWith('/v1/auth/firebase/verify')) {
          const body = JSON.parse(String(init?.body)) as { idToken: string; app: string };
          expect(body.idToken).toBe('id-token');
          expect(body.app).toBe('customer');
          return verifyStatus === 200
            ? json(verifyBody)
            : new Response(JSON.stringify(verifyBody), {
                status: verifyStatus,
                headers: { 'content-type': 'application/problem+json' },
              });
        }
        return json({ requestId: 'req-1', resendAfter: 30 });
      }) as typeof fetch,
    });
  });
  afterEach(() => {
    Reflect.deleteProperty(window, 'OTPCredential');
    configure({});
  });

  async function sendCode(l: PhoneOtpLabels = labels()) {
    const session = { app: 'customer', signIn: vi.fn() } as unknown as Session;
    const onSignedIn = vi.fn();
    render(
      <PhoneOtpFlow session={session} siteKey={TURNSTILE_TEST_SITE_KEY} labels={l} onSignedIn={onSignedIn} />,
    );
    await screen.findByText('Protected by reCAPTCHA');
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/Sent to/);
    return { session, onSignedIn };
  }

  function enter(code: string) {
    const first = screen.getByRole('group', { name: 'Enter the 6-digit code' }).querySelector('input') as HTMLInputElement;
    fireEvent.change(first, { target: { value: code } });
  }

  const fbLabels = (): PhoneOtpLabels => ({
    ...labels(),
    recaptchaNotice: 'Protected by reCAPTCHA',
    languageCode: 'hi',
    errors: { ...labels().errors, methodChanged: 'Method changed' },
  });

  it('sends through Firebase, swaps the ID token and signs in, with no otp/send or WebOTP', async () => {
    const { session, onSignedIn } = await sendCode(fbLabels());
    expect(firebase.send).toHaveBeenCalledWith('+919000000011', 'hi');
    expect(screen.getByRole('button', { name: /Resend in 30 s/ })).toBeDisabled();
    enter('000000');
    await waitFor(() => expect(onSignedIn).toHaveBeenCalledWith({ userId: 'u1', isNewUser: true }));
    expect(session.signIn).toHaveBeenCalledWith({ accessToken: 'at', expiresAt: '2026-10-27T10:15:00Z', userId: 'u1' });
    expect(calls).toEqual(['/v1/auth/config', '/v1/auth/firebase/verify']);
    expect(get).not.toHaveBeenCalled();
  });

  it('shows a wrong code from Firebase without calling the API', async () => {
    const { session } = await sendCode(fbLabels());
    enter('123456');
    await screen.findByText('w');
    expect(calls).toEqual(['/v1/auth/config']);
    expect(session.signIn).not.toHaveBeenCalled();
  });

  it('maps Firebase send failures to the phone step messages', async () => {
    const { FirebasePhoneError } = await import('./firebase');
    firebase.send.mockRejectedValueOnce(new FirebasePhoneError('rateLimited'));
    render(
      <PhoneOtpFlow
        session={{ app: 'customer', signIn: vi.fn() } as unknown as Session}
        siteKey={TURNSTILE_TEST_SITE_KEY}
        labels={fbLabels()}
        onSignedIn={() => undefined}
      />,
    );
    await screen.findByText('Protected by reCAPTCHA');
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText('r');
    firebase.send.mockRejectedValueOnce(new FirebasePhoneError('captcha'));
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText('c');
  });

  it('goes back to the number with a message when the deploy switched provider', async () => {
    verifyStatus = 409;
    verifyBody = { type: 'about:blank', code: 'sign_in_method_changed', status: 409, title: 'x' };
    await sendCode(fbLabels());
    enter('000000');
    await screen.findByText('Method changed');
    expect(screen.getByLabelText('Mobile number')).toBeInTheDocument();
    await waitFor(() => expect(calls.filter((c) => c === '/v1/auth/config')).toHaveLength(2));
  });

  it('falls back to SMS when the config cannot be read', async () => {
    configure({
      baseUrl: 'https://api.test',
      fetch: (async (input: RequestInfo | URL) => {
        const url = String(input);
        calls.push(url.replace('https://api.test', ''));
        if (url.endsWith('/v1/auth/config')) return new Response('down', { status: 503 });
        return json({ requestId: 'req-1', resendAfter: 30 });
      }) as typeof fetch,
    });
    render(
      <PhoneOtpFlow
        session={{ app: 'customer', signIn: vi.fn() } as unknown as Session}
        siteKey={TURNSTILE_TEST_SITE_KEY}
        labels={fbLabels()}
        onSignedIn={() => undefined}
      />,
    );
    fireEvent.change(screen.getByLabelText('Mobile number'), { target: { value: '9000000011' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/Sent to/);
    expect(calls).toEqual(['/v1/auth/config', '/v1/auth/otp/send']);
    expect(firebase.load).not.toHaveBeenCalled();
    expect(screen.queryByText('Protected by reCAPTCHA')).toBeNull();
  });
});
