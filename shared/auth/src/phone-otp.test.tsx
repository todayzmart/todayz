import { configure } from '@todayz/api-client';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PhoneOtpFlow, type PhoneOtpLabels } from './phone-otp';
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
    expect(get).toHaveBeenCalledTimes(1);
    view.rerender(ui(() => undefined));
    view.rerender(ui(() => undefined));
    expect(get).toHaveBeenCalledTimes(1);
  });
});
