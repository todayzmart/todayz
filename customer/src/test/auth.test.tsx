import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Handler } from './render';

const PHONE = '9000000011';
const TOKEN = 'tok-secret-123';

const me = { id: '01J9ZAAAAAAAAAAAAAAAAAAAAA', name: null, preferredLang: 'en', phoneMasked: '90•••••011', app: 'customer', createdAt: '2026-10-01T00:00:00Z' };

function authHandlers(isNewUser: boolean, extra: Handler[] = []): Handler[] {
  return [
    (c) => (c.path === '/v1/auth/otp/send' ? json(200, { requestId: 'req-1', resendAfter: 30 }) : undefined),
    (c) =>
      c.path === '/v1/auth/otp/verify'
        ? (c.body as { code: string }).code === '123456'
          ? json(200, { accessToken: TOKEN, expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: me.id, isNewUser })
          : problem(401, 'unauthorized')
        : undefined,
    (c) => (c.path === '/v1/me' && c.method === 'GET' ? json(200, me, { etag: '"v1"' }) : undefined),
    (c) =>
      c.path === '/v1/me' && c.method === 'PATCH'
        ? c.headers.get('if-match') === '"v1"'
          ? json(200, { ...me, ...(c.body as object) }, { etag: '"v2"' })
          : problem(412, 'precondition_failed')
        : undefined,
    ...extra,
  ];
}

async function enterPhoneAndCode(code = '123456') {
  fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: PHONE } });
  fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
  await screen.findByText(/We sent a code to/);
  const boxes = screen.getByRole('group', { name: 'Enter the 6-digit code' }).querySelectorAll('input');
  for (const [i, d] of code.split('').entries()) {
    fireEvent.change(boxes[i] as HTMLInputElement, { target: { value: d } });
  }
}

describe('C-10 sign in', () => {
  it('sends the code without a bearer, verifies, and lands on the next page', async () => {
    const { calls, session } = renderAt('/sign-in?next=/search', { handlers: authHandlers(false) });
    await enterPhoneAndCode();
    expect(await screen.findByRole('heading', { level: 1, name: 'Search' })).toBeInTheDocument();

    const send = calls.find((c) => c.path === '/v1/auth/otp/send');
    expect(send?.body).toMatchObject({ phone: `+91${PHONE}`, purpose: 'login', app: 'customer', turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' });
    for (const c of calls.filter((x) => x.path.startsWith('/v1/auth/'))) expect(c.headers.get('authorization')).toBeNull();
    expect(session.getState().status).toBe('signedIn');
  });

  it('keeps the token in memory only', async () => {
    renderAt('/sign-in', { handlers: authHandlers(false) });
    await enterPhoneAndCode();
    await waitFor(() => expect(screen.queryByLabelText('Mobile number')).toBeNull());
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const key = store.key(i) ?? '';
        expect(key + (store.getItem(key) ?? '')).not.toContain(TOKEN);
      }
    }
  });

  it('shows a translated error for a wrong code and lets the person retry', async () => {
    renderAt('/sign-in', { handlers: authHandlers(false) });
    await enterPhoneAndCode('000000');
    expect(await screen.findByText('That code is not right. Check it and try again.')).toBeInTheDocument();
  });

  it('rejects a number that is not 10 digits before calling the API', async () => {
    const { calls } = renderAt('/sign-in', { handlers: authHandlers(false) });
    fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: '12345' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    expect(await screen.findByText(/Enter a 10-digit mobile number/)).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/auth/otp/send')).toBe(false);
  });

  it('a new account may add a name (PATCH with If-Match) or skip', async () => {
    const { calls } = renderAt('/sign-in', { handlers: authHandlers(true) });
    await enterPhoneAndCode();
    fireEvent.change(await screen.findByLabelText('Your name (optional)'), { target: { value: 'Asha' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch?.body).toEqual({ name: 'Asha' });
    expect(patch?.headers.get('if-match')).toBe('"v1"');
    expect(await screen.findByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeInTheDocument();
  });

  it('never follows a next= that leaves the app', async () => {
    renderAt('/sign-in?next=//evil.example', { handlers: authHandlers(false) });
    await enterPhoneAndCode();
    expect(await screen.findByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeInTheDocument();
  });
});

describe('resend on the code step (review 9)', () => {
  it('shows a resend failure on the code step', async () => {
    let sends = 0;
    renderAt('/sign-in', {
      handlers: [
        (c) => {
          if (c.path !== '/v1/auth/otp/send') return undefined;
          sends += 1;
          return sends === 1 ? json(200, { requestId: 'req-1', resendAfter: 0 }) : problem(429, 'rate_limited');
        },
      ],
    });
    fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: PHONE } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    await screen.findByText(/We sent a code to/);
    fireEvent.click(screen.getByRole('button', { name: 'Resend code' }));
    expect(await screen.findByText('Too many tries. Wait a moment and try again.')).toBeInTheDocument();
    expect(screen.getByText(/We sent a code to/)).toBeInTheDocument();
  });
});

describe('user switch (review 8)', () => {
  it('clears the query cache when another tab signs in as someone else', async () => {
    const { session, queryClient } = renderAt('/account', { handlers: authHandlers(false) });
    const jwt = (sub: string) => `${btoa('{}')}.${btoa(JSON.stringify({ sub }))}.s`;
    const exp = new Date(Date.now() + 900_000).toISOString();
    session.signIn({ accessToken: jwt('u1'), expiresAt: exp });
    queryClient.setQueryData(['me'], { id: 'u1' });
    session.signIn({ accessToken: jwt('u2'), expiresAt: exp });
    expect(queryClient.getQueryData(['me'])).toBeUndefined();
  });
});

describe('Account', () => {
  it('offers sign in when signed out', async () => {
    renderAt('/account');
    expect(await screen.findByRole('heading', { name: 'Sign in to TodayZ' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/sign-in?next=%2Faccount');
  });

  it('shows the profile, saves a name with If-Match, and reloads on a 412', async () => {
    let conflict = true;
    const { calls } = renderAt('/account', {
      returning: true,
      handlers: [
        (c) =>
          c.path === '/v1/auth/refresh'
            ? json(200, { accessToken: TOKEN, expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: me.id })
            : undefined,
        (c) =>
          c.path === '/v1/me' && c.method === 'PATCH' && conflict
            ? ((conflict = false), problem(412, 'precondition_failed'))
            : undefined,
        ...authHandlers(false),
      ],
    });
    expect(await screen.findByText('90•••••011')).toBeInTheDocument();
    const name = screen.getByLabelText('Name');
    fireEvent.change(name, { target: { value: 'Asha' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/changed on another device/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Asha' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Saved')).toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'PATCH')).toHaveLength(2);
  });

  it('signs out', async () => {
    const { calls, session } = renderAt('/account', {
      returning: true,
      handlers: [
        (c) =>
          c.path === '/v1/auth/refresh'
            ? json(200, { accessToken: TOKEN, expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: me.id })
            : undefined,
        (c) => (c.path === '/v1/auth/logout' ? new Response(null, { status: 204 }) : undefined),
        ...authHandlers(false),
      ],
    });
    await screen.findByText('90•••••011');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    });
    expect(await screen.findByRole('heading', { name: 'Sign in to TodayZ' })).toBeInTheDocument();
    expect(session.getState().status).toBe('signedOut');
    const logout = calls.find((c) => c.path === '/v1/auth/logout');
    expect(logout?.body).toEqual({ app: 'customer' });
    expect(logout?.headers.get('authorization')).toBeNull();
  });
});
