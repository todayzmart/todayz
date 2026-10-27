import { configure } from '@todayz/api-client';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PhoneOtpFlow, otpErrorMessage, type PhoneOtpLabels } from './phone-otp';
import type { Session } from './session';
import { TURNSTILE_TEST_SITE_KEY } from './turnstile';

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
      fetch: (async () => {
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
