import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ApiError } from '@todayz/api-client';
import { totpError } from '../app/totp';
import { createSecondFactor } from '../app/second-factor';
import { json, problem, renderAt, type Handler } from './render';

const PHONE = '9000000012';
const OTP_TOKEN = 'tok-otp-456';
const FULL_TOKEN = 'tok-full-789';
const CODES = ['K7M2Q-XB4NP', 'R9TDA-5HWZC', 'M3PQK-8VYXE', 'A2CDE-FGHJK', 'MNPQR-STUVW', 'XYZ23-45678', '9ABCD-EFGHJ', 'KMNPQ-RSTUV', 'WXYZ2-3456A', 'BCDEF-GHJKM'];

const grant = (token: string, extra: Record<string, unknown> = {}) => ({
  accessToken: token,
  expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
  tokenType: 'Bearer',
  ...extra,
});

function otpHandlers(nextStep: 'totp_enrol' | 'totp_verify', extra: Handler[] = []): Handler[] {
  return [
    (c) => (c.path === '/v1/auth/otp/send' ? json(200, { requestId: 'req-1', resendAfter: 30 }) : undefined),
    (c) =>
      c.path === '/v1/auth/otp/verify'
        ? (c.body as { code: string }).code === '123456'
          ? json(200, grant(OTP_TOKEN, { userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA', isNewUser: false, nextStep }))
          : problem(401, 'unauthorized')
        : undefined,
    ...extra,
  ];
}

async function typeCode(group: string, code: string) {
  const boxes = (await screen.findByRole('group', { name: group })).querySelectorAll('input');
  for (const [i, d] of code.split('').entries()) fireEvent.change(boxes[i] as HTMLInputElement, { target: { value: d } });
}

async function phoneStep() {
  fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: PHONE } });
  fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
  await screen.findByText(/We sent a code to/);
  await typeCode('Enter the 6-digit code', '123456');
}

const enrolOk: Handler = (c) =>
  c.path === '/v1/auth/totp/enrol'
    ? json(200, { secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP', otpauthUri: 'otpauth://totp/TodayZ:01J?secret=JBSWY3DPEHPK3PXP', recoveryCodes: CODES })
    : undefined;

const verifyOk: Handler = (c) => (c.path === '/v1/auth/totp/verify' ? json(200, grant(FULL_TOKEN)) : undefined);

describe('A-01 gate', () => {
  it('sends a signed-out visitor to sign in and remembers where they were going', async () => {
    const { history } = renderAt('/businesses', { signedIn: false });
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to TodayZ Admin' })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/sign-in');
    expect(history.location.search).toContain('next=');
    expect(screen.queryByRole('navigation', { name: 'Main' })).toBeNull();
  });

  it('shows no app chrome while the cookie is being tried', () => {
    renderAt('/', { signedIn: false });
    expect(screen.queryByRole('navigation', { name: 'Main' })).toBeNull();
  });

  it('holds a signed-in person at the two-step check until the authenticator is done', async () => {
    const { history } = renderAt('/stores', { secondFactor: false, handlers: [(c) => (c.path === '/v1/auth/totp/enrol' ? problem(409, 'conflict') : undefined)] });
    expect(await screen.findByRole('heading', { level: 1, name: 'Two-step check' })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/two-step');
    expect(screen.queryByRole('navigation', { name: 'Main' })).toBeNull();
  });

  it('never follows a next= that leaves the app', async () => {
    const { history } = renderAt('/sign-in?next=//evil.example', { signedIn: false, handlers: otpHandlers('totp_verify') });
    await phoneStep();
    await waitFor(() => expect(history.location.pathname).toBe('/two-step'));
    expect(history.location.search).not.toContain('evil');
  });
});

describe('A-01 phone step', () => {
  it('sends the code for the admin app without a bearer', async () => {
    const { calls } = renderAt('/sign-in', { signedIn: false, handlers: otpHandlers('totp_verify') });
    await phoneStep();
    await screen.findByRole('heading', { level: 1, name: 'Two-step check' });
    expect(calls.find((c) => c.path === '/v1/auth/otp/send')?.body).toMatchObject({ phone: `+91${PHONE}`, purpose: 'login', app: 'admin' });
    for (const c of calls.filter((x) => x.path.startsWith('/v1/auth/otp'))) expect(c.headers.get('authorization')).toBeNull();
  });

  it('shows a translated error for a wrong code', async () => {
    renderAt('/sign-in', { signedIn: false, handlers: otpHandlers('totp_verify') });
    fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: PHONE } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/We sent a code to/);
    await typeCode('Enter the 6-digit code', '000000');
    expect(await screen.findByText('That code is not right. Check it and try again.')).toBeInTheDocument();
  });
});

describe('A-01 first sign-in: enrol', () => {
  it('shows the setup key, then the recovery codes once, then Pulse', async () => {
    const { calls, session, history } = renderAt('/sign-in', { signedIn: false, handlers: otpHandlers('totp_enrol', [enrolOk, verifyOk]) });
    await phoneStep();
    expect(await screen.findByTestId('totp-secret')).toHaveTextContent('JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP');
    // The setup call carries the OTP-step bearer.
    expect(calls.find((c) => c.path === '/v1/auth/totp/enrol')?.headers.get('authorization')).toBe(`Bearer ${OTP_TOKEN}`);

    await typeCode('Authenticator code', '654321');
    expect(await screen.findByRole('heading', { level: 1, name: 'Save your recovery codes' })).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/auth/totp/verify')?.body).toEqual({ code: '654321', purpose: 'login' });
    expect(session.getToken()).toBe(FULL_TOKEN);
    expect(screen.getByRole('list', { name: 'Recovery codes' }).querySelectorAll('li')).toHaveLength(10);

    // Not stored anywhere, and the page cannot be left before the person confirms.
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const key = store.key(i) ?? '';
        expect(key + (store.getItem(key) ?? '')).not.toContain('K7M2Q');
      }
    }
    const next = screen.getByRole('button', { name: 'Continue to “Pulse”' });
    expect(next).toBeDisabled();
    fireEvent.click(screen.getByLabelText('I have saved these codes'));
    fireEvent.click(next);
    await waitFor(() => expect(history.location.pathname).toBe('/'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Pulse' })).toBeInTheDocument();
    expect(screen.queryByText('K7M2Q-XB4NP')).toBeNull();
  });

  it('asks for the code instead when the admin is already enrolled', async () => {
    renderAt('/sign-in', {
      signedIn: false,
      handlers: otpHandlers('totp_enrol', [(c) => (c.path === '/v1/auth/totp/enrol' ? problem(409, 'conflict') : undefined)]),
    });
    await phoneStep();
    expect(await screen.findByText(/Open your authenticator app/)).toBeInTheDocument();
  });

  it('says so when the setup is not available, and tries again on request', async () => {
    let ok = false;
    renderAt('/sign-in', {
      signedIn: false,
      handlers: otpHandlers('totp_enrol', [(c) => (c.path === '/v1/auth/totp/enrol' ? (ok ? enrolOk(c) : problem(404, 'not_found')) : undefined)]),
    });
    await phoneStep();
    expect(await screen.findByText(/two-step check is not available yet/)).toBeInTheDocument();
    ok = true;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByTestId('totp-secret')).toBeInTheDocument();
  });
});

describe('A-01 returning sign-in: verify', () => {
  it('takes the authenticator code and opens Pulse behind the shell', async () => {
    const { session, history } = renderAt('/sign-in?next=/stores', { signedIn: false, handlers: otpHandlers('totp_verify', [verifyOk]) });
    await phoneStep();
    await screen.findByText(/Open your authenticator app/);
    await typeCode('Authenticator code', '111222');
    await waitFor(() => expect(history.location.pathname).toBe('/stores'));
    expect(session.getToken()).toBe(FULL_TOKEN);
    expect(await screen.findByRole('navigation', { name: 'Main' })).toBeInTheDocument();
  });

  it('shows the tries left for a wrong code', async () => {
    renderAt('/two-step', {
      secondFactor: false,
      handlers: [
        (c) =>
          c.path === '/v1/auth/totp/verify'
            ? new Response(
                JSON.stringify({ type: 'about:blank#code_invalid', title: 'x', status: 422, code: 'code_invalid', extensions: { attemptsLeft: 2 } }),
                { status: 422, headers: { 'content-type': 'application/problem+json' } },
              )
            : undefined,
      ],
    });
    await typeCode('Authenticator code', '000000');
    expect(await screen.findByText(/2 tries left/)).toBeInTheDocument();
  });

  it('ends at a sign-in again prompt after too many wrong codes', async () => {
    renderAt('/two-step', { secondFactor: false, handlers: [(c) => (c.path === '/v1/auth/totp/verify' ? problem(429, 'otp_locked') : undefined)] });
    await typeCode('Authenticator code', '000000');
    expect(await screen.findByText('Too many wrong codes. Sign in again.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign in again' })).toBeInTheDocument();
  });

  it('accepts a recovery code and says how many are left', async () => {
    const { calls, session } = renderAt('/two-step', {
      secondFactor: false,
      handlers: [(c) => (c.path === '/v1/auth/totp/recover' ? json(200, grant(FULL_TOKEN, { recoveryCodesLeft: 9 })) : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Use a recovery code instead' }));
    fireEvent.change(screen.getByLabelText('Recovery code'), { target: { value: 'K7M2Q-XB4NP' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use recovery code' }));
    expect(await screen.findByText(/9 recovery codes left/)).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/auth/totp/recover')?.body).toEqual({ recoveryCode: 'K7M2Q-XB4NP' });
    expect(session.getToken()).toBe(FULL_TOKEN);
  });

  it('refuses a wrong recovery code', async () => {
    renderAt('/two-step', { secondFactor: false, handlers: [(c) => (c.path === '/v1/auth/totp/recover' ? problem(422, 'code_invalid') : undefined)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Use a recovery code instead' }));
    fireEvent.change(screen.getByLabelText('Recovery code'), { target: { value: 'NOPE-NOPE-1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use recovery code' }));
    expect(await screen.findByText('That recovery code is not right or was already used.')).toBeInTheDocument();
  });
});

describe('sign out', () => {
  it('signs out, forgets the authenticator step and returns to sign in', async () => {
    const { session, history } = renderAt('/', {
      handlers: [(c) => (c.path === '/v1/auth/logout' ? new Response(null, { status: 204 }) : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    await waitFor(() => expect(session.getState().status).toBe('signedOut'));
    await waitFor(() => expect(history.location.pathname).toBe('/sign-in'));
    expect(sessionStorage.getItem('todayz.admin.2fa')).toBeNull();
  });
});

describe('review fixes', () => {
  it('makes the otpauth link clickable only for an otpauth:// address', async () => {
    const bad: Handler = (c) =>
      c.path === '/v1/auth/totp/enrol'
        ? json(200, { secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP', otpauthUri: 'javascript:alert(1)', recoveryCodes: CODES })
        : undefined;
    renderAt('/sign-in', { signedIn: false, handlers: otpHandlers('totp_enrol', [bad, verifyOk]) });
    await phoneStep();
    await screen.findByTestId('totp-secret');
    expect(document.querySelector('a[href^="javascript"]')).toBeNull();
    expect(screen.queryByRole('link', { name: /authenticator app/i })).toBeNull();
  });

  it('opens the otpauth link when the address is otpauth://', async () => {
    renderAt('/sign-in', { signedIn: false, handlers: otpHandlers('totp_enrol', [enrolOk, verifyOk]) });
    await phoneStep();
    await screen.findByTestId('totp-secret');
    expect(document.querySelector('a[href^="otpauth://"]')).not.toBeNull();
  });

  it('maps step_up_required to its own kind and keeps attemptsLeft for a wrong code', () => {
    const mk = (status: number, code: string, extensions?: Record<string, unknown>) =>
      new ApiError({ type: 't', title: 't', status, code, ...(extensions ? { extensions } : {}) } as never, new Headers());
    expect(totpError(mk(403, 'step_up_required'))).toEqual({ kind: 'stepUp' });
    expect(totpError(mk(429, 'otp_locked'))).toEqual({ kind: 'locked' });
    expect(totpError(mk(422, 'code_invalid', { attemptsLeft: 3 }))).toEqual({ kind: 'wrong', attemptsLeft: 3 });
  });

  it('keeps nextStep across a reload and forgets it on clear', () => {
    sessionStorage.clear();
    const a = createSecondFactor();
    a.setNextStep('totp_enrol');
    expect(createSecondFactor().getState().nextStep).toBe('totp_enrol');
    a.clear();
    expect(createSecondFactor().getState().nextStep).toBeUndefined();
  });

  it('forgets the authenticator step when another tab signs in as someone else', () => {
    const { session, factor } = renderAt('/', {});
    const jwt = (sub: string) => `${btoa('{}')}.${btoa(JSON.stringify({ sub }))}.s`;
    const exp = new Date(Date.now() + 900_000).toISOString();
    session.signIn({ accessToken: jwt('a1'), expiresAt: exp });
    factor.markDone();
    expect(factor.getState().done).toBe(true);
    session.signIn({ accessToken: jwt('a2'), expiresAt: exp });
    expect(factor.getState().done).toBe(false);
  });
});
