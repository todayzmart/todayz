import en from '@todayz/i18n/compiled/admin/en.json';
import { createSession, SessionProvider } from '@todayz/auth';
import { asMessages, FORMAT_LOCALE } from '@todayz/i18n';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { IntlProvider } from 'react-intl';
import { describe, expect, it, vi } from 'vitest';
import { StepUpDialog } from '../app/StepUpDialog';
import { json, problem, renderAt, type Call, type Handler } from './render';
import { STEP_UP_CODE, STEP_UP_REQUEST, sendStepUpCode, stepUpHandler, typeStepUpCode } from './step-up';

const PHONE = '9000000012';
const OTP_TOKEN = 'tok-otp-456';

const grant = (token: string, extra: Record<string, unknown> = {}) => ({
  accessToken: token,
  expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
  tokenType: 'Bearer',
  ...extra,
});

/** The sign-in routes (D-264): the SMS code alone answers the admin's token, with no second step. */
function otpHandlers(extra: Handler[] = []): Handler[] {
  return [
    (c) => (c.path === '/v1/auth/otp/send' ? json(200, { requestId: 'req-1', resendAfter: 30 }) : undefined),
    (c) =>
      c.path === '/v1/auth/otp/verify'
        ? (c.body as { code: string }).code === '123456'
          ? json(200, grant(OTP_TOKEN, { userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA', isNewUser: false }))
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

  it('lets a signed-in admin straight into the shell; there is no two-step page any more (D-264)', async () => {
    const { history } = renderAt('/two-step');
    expect(await screen.findByRole('heading', { level: 1, name: 'Page not found' })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/two-step');
    expect(screen.getByRole('navigation', { name: 'Main' })).toBeInTheDocument();
  });

  it('never follows a next= that leaves the app', async () => {
    const { history } = renderAt('/sign-in?next=//evil.example', { signedIn: false, handlers: otpHandlers() });
    await phoneStep();
    await waitFor(() => expect(history.location.pathname).toBe('/'));
    expect(history.location.search).not.toContain('evil');
  });
});

describe('A-01 phone step', () => {
  it('sends the code for the admin app without a bearer', async () => {
    const { calls } = renderAt('/sign-in', { signedIn: false, handlers: otpHandlers() });
    await phoneStep();
    await screen.findByRole('heading', { level: 1, name: 'Pulse' });
    expect(calls.find((c) => c.path === '/v1/auth/otp/send')?.body).toMatchObject({ phone: `+91${PHONE}`, purpose: 'login', app: 'admin' });
    for (const c of calls.filter((x) => x.path.startsWith('/v1/auth/otp'))) expect(c.headers.get('authorization')).toBeNull();
  });

  it('shows a translated error for a wrong code', async () => {
    renderAt('/sign-in', { signedIn: false, handlers: otpHandlers() });
    fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: PHONE } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/We sent a code to/);
    await typeCode('Enter the 6-digit code', '000000');
    expect(await screen.findByText('That code is not right. Check it and try again.')).toBeInTheDocument();
  });
});

describe('A-01 the SMS code alone signs an admin in (D-264)', () => {
  it('opens Pulse with the sign-in token and calls no authenticator route', async () => {
    const { calls, history, session } = renderAt('/sign-in', { signedIn: false, handlers: otpHandlers() });
    await phoneStep();
    await waitFor(() => expect(history.location.pathname).toBe('/'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Pulse' })).toBeInTheDocument();
    expect(session.getToken()).toBe(OTP_TOKEN);
    expect(calls.some((c) => c.path.startsWith('/v1/auth/totp/') || c.path.startsWith('/v1/auth/step-up/'))).toBe(false);
  });

  it('goes on to the page the admin was opening', async () => {
    const { history } = renderAt('/sign-in?next=/stores', { signedIn: false, handlers: otpHandlers() });
    await phoneStep();
    await waitFor(() => expect(history.location.pathname).toBe('/stores'));
    expect(await screen.findByRole('navigation', { name: 'Main' })).toBeInTheDocument();
  });
});

describe('sign out', () => {
  it('signs out and returns to sign in', async () => {
    const { session, history } = renderAt('/', {
      handlers: [(c) => (c.path === '/v1/auth/logout' ? new Response(null, { status: 204 }) : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    await waitFor(() => expect(session.getState().status).toBe('signedOut'));
    await waitFor(() => expect(history.location.pathname).toBe('/sign-in'));
  });
});

/** Renders the step-up dialog alone, signed in, on a fake API. */
function renderStepUp(handlers: Handler[]) {
  const calls: Call[] = [];
  const fakeFetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url);
    const raw = typeof init?.body === 'string' ? init.body : undefined;
    const call: Call = {
      method: (init?.method ?? 'GET').toUpperCase(),
      path: url.pathname,
      search: url.search,
      headers: new Headers(init?.headers),
      body: raw ? (JSON.parse(raw) as unknown) : undefined,
    };
    calls.push(call);
    for (const h of handlers) {
      const r = h(call);
      if (r) return r;
    }
    if (call.path === '/v1/auth/logout') return new Response(null, { status: 204 });
    return problem(404, 'not_found');
  }) as typeof fetch;
  const session = createSession({ app: 'admin', baseUrl: 'https://api.test', clientId: 'admin/test', fetch: fakeFetch, locks: null, channel: null, graceWaitMs: 0 });
  session.signIn({ accessToken: 'test-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), userId: 'u1' });
  const onVerified = vi.fn();
  const onClose = vi.fn();
  const view = render(
    <IntlProvider locale={FORMAT_LOCALE} defaultLocale={FORMAT_LOCALE} messages={asMessages(en)} onError={(e) => { throw e; }}>
      <QueryClientProvider client={new QueryClient()}>
        <SessionProvider session={session}>
          <StepUpDialog open onVerified={onVerified} onClose={onClose} />
        </SessionProvider>
      </QueryClientProvider>
    </IntlProvider>,
  );
  return { ...view, calls, session, onVerified, onClose };
}

const verifyAnswers = (r: Response): Handler => (c) => (c.path === '/v1/auth/step-up/verify' ? r : undefined);

describe('step-up by SMS (D-264)', () => {
  it('sends nothing until the admin asks, then takes the code with the bearer and the request id', async () => {
    const state = { stepUpNeeded: true };
    const { calls, onVerified } = renderStepUp([stepUpHandler(state)]);
    await screen.findByRole('dialog', { name: 'Confirm it is you' });
    expect(calls.some((c) => c.path.startsWith('/v1/auth/step-up/'))).toBe(false);
    await sendStepUpCode();
    await typeStepUpCode(STEP_UP_CODE);
    await waitFor(() => expect(onVerified).toHaveBeenCalledTimes(1));
    const send = calls.find((c) => c.path === '/v1/auth/step-up/send');
    const verify = calls.find((c) => c.path === '/v1/auth/step-up/verify');
    expect(send?.headers.get('authorization')).toBe('Bearer test-token');
    expect(send?.body ?? null).toBeNull(); // the API picks the admin's own number; the client names none
    expect(verify?.body).toEqual({ requestId: STEP_UP_REQUEST, code: STEP_UP_CODE });
    expect(state.stepUpNeeded).toBe(false);
  });

  it('says an expired or used code is gone and lets the admin send a new one', async () => {
    const { onVerified } = renderStepUp([verifyAnswers(problem(401, 'otp_expired')), stepUpHandler({ stepUpNeeded: true })]);
    const dialog = await sendStepUpCode();
    await typeStepUpCode('111111');
    expect(await within(dialog).findByText('That code has expired or was already used. Send a new code.')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'New code in 30 s' })).toBeDisabled();
    expect(onVerified).not.toHaveBeenCalled();
  });

  it('ends at "Sign in again" after the fifth wrong code, which signs the admin out', async () => {
    const { session, onVerified } = renderStepUp([verifyAnswers(problem(429, 'otp_locked')), stepUpHandler({ stepUpNeeded: true })]);
    const dialog = await sendStepUpCode();
    await typeStepUpCode('222222');
    expect(await within(dialog).findByText('Too many wrong codes. You have been signed out; sign in again.')).toBeInTheDocument();
    expect(within(dialog).queryByRole('button', { name: 'Send a new code' })).toBeNull();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Sign in again' }));
    await waitFor(() => expect(session.getState().status).toBe('signedOut'));
    expect(onVerified).not.toHaveBeenCalled();
  });

  it('waits out Retry-After when too many codes were asked for', async () => {
    const { calls } = renderStepUp([
      (c) => (c.path === '/v1/auth/step-up/send' ? json(429, { type: 'about:blank#rate_limited', title: 'x', status: 429, code: 'rate_limited' }, { 'retry-after': '120' }) : undefined),
    ]);
    const dialog = await screen.findByRole('dialog', { name: 'Confirm it is you' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Send code' }));
    expect(await within(dialog).findByText('Too many codes asked for. Wait before you ask for another.')).toBeInTheDocument();
    const again = within(dialog).getByRole('button', { name: 'New code in 120 s' });
    expect(again).toBeDisabled();
    fireEvent.click(again);
    expect(calls.filter((c) => c.path === '/v1/auth/step-up/send')).toHaveLength(1);
  });

  it('says so when the code cannot be sent', async () => {
    renderStepUp([(c) => (c.path === '/v1/auth/step-up/send' ? problem(503, 'unavailable') : undefined)]);
    const dialog = await screen.findByRole('dialog', { name: 'Confirm it is you' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Send code' }));
    expect(await within(dialog).findByText("We couldn't send or check the code. Try again in a moment.")).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Send code' })).toBeEnabled();
  });
});
