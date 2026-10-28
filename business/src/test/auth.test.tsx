import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { insights, overviewHandler } from './insightsApi';
import { json, problem, renderAt, type Handler } from './render';

const PHONE = '9000000012';
const TOKEN = 'tok-secret-456';

const GRANT = (): Record<string, unknown> => ({
  accessToken: TOKEN,
  expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
  tokenType: 'Bearer',
  userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
  isNewUser: false,
});

function authHandlers(extra: Handler[] = []): Handler[] {
  return [
    (c) => (c.path === '/v1/auth/otp/send' ? json(200, { requestId: 'req-1', resendAfter: 30 }) : undefined),
    (c) =>
      c.path === '/v1/auth/otp/verify'
        ? (c.body as { code: string }).code === '123456'
          ? json(200, GRANT())
          : problem(401, 'unauthorized')
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

const shops = (items: unknown[]): Handler => (c) => (c.path === '/v1/biz/businesses' ? json(200, { items, invitations: [] }) : undefined);

describe('B-01 gate', () => {
  it('sends a signed-out visitor to sign in and remembers where they were going', async () => {
    const { history } = renderAt('/orders', { signedIn: false });
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to TodayZ Business' })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/sign-in');
    expect(history.location.search).toContain('next=');
    expect(screen.queryByRole('navigation', { name: 'Business' })).toBeNull();
  });

  it('shows no app chrome while the cookie is being tried', () => {
    renderAt('/', { signedIn: false });
    expect(screen.queryByRole('navigation', { name: 'Business' })).toBeNull();
  });

  it('never follows a next= that leaves the app', async () => {
    const { history } = renderAt('/sign-in?next=//evil.example', { signedIn: false, handlers: authHandlers([shops([])]) });
    await enterPhoneAndCode();
    await waitFor(() => expect(history.location.pathname).toBe('/choose-shop'));
    expect(history.location.search).not.toContain('evil');
  });
});

describe('B-01 sign in', () => {
  it('sends the code without a bearer and verifies it for the business app', async () => {
    const { calls, session } = renderAt('/sign-in', {
      signedIn: false,
      handlers: authHandlers([shops([{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'owner', stores: [{ id: 's1', name: 'Indiranagar' }] }]), overviewHandler(), insights()]),
    });
    await enterPhoneAndCode();
    // Lands on the Overview, which now reads GET /overview.
    expect(await screen.findByRole('heading', { level: 1, name: /^Good / })).toBeInTheDocument();
    const send = calls.find((c) => c.path === '/v1/auth/otp/send');
    expect(send?.body).toMatchObject({ phone: `+91${PHONE}`, purpose: 'login', app: 'business', turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' });
    for (const c of calls.filter((x) => x.path.startsWith('/v1/auth/'))) expect(c.headers.get('authorization')).toBeNull();
    expect(session.getState().status).toBe('signedIn');
  });

  it('keeps the token in memory only', async () => {
    renderAt('/sign-in', { signedIn: false, handlers: authHandlers([shops([])]) });
    await enterPhoneAndCode();
    await waitFor(() => expect(screen.queryByLabelText('Mobile number')).toBeNull());
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const key = store.key(i) ?? '';
        expect(key + (store.getItem(key) ?? '')).not.toContain(TOKEN);
      }
    }
  });

  it('shows a translated error for a wrong code', async () => {
    renderAt('/sign-in', { signedIn: false, handlers: authHandlers() });
    await enterPhoneAndCode('000000');
    expect(await screen.findByText('That code is not right. Check it and try again.')).toBeInTheDocument();
  });

  it('rejects a number that is not 10 digits before calling the API', async () => {
    const { calls } = renderAt('/sign-in', { signedIn: false, handlers: authHandlers() });
    fireEvent.change(await screen.findByLabelText('Mobile number'), { target: { value: '12345' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }));
    expect(await screen.findByText(/Enter a 10-digit mobile number/)).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/auth/otp/send')).toBe(false);
  });
});

describe('B-01 shop picker', () => {
  it('lists businesses with the role and remembers the chosen store', async () => {
    const { history } = renderAt('/choose-shop?next=/orders', {
      handlers: [
        shops([
          { id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'owner', stores: [{ id: 's1', name: 'Indiranagar' }, { id: 's2', name: 'Koramangala' }] },
          { id: 'b2', status: 'verified', brandName: 'Fresh Mart', role: 'cashier', stores: [{ id: 's3', name: 'HSR' }] },
        ]),
      ],
    });
    const first = await screen.findByRole('region', { name: 'Stores of Sri Lakshmi' });
    expect(within(first).getByText('Owner')).toBeInTheDocument();
    expect(within(screen.getByRole('region', { name: 'Stores of Fresh Mart' })).getByText('Cashier')).toBeInTheDocument();
    fireEvent.click(within(first).getByRole('button', { name: 'Koramangala' }));
    await waitFor(() => expect(history.location.pathname).toBe('/orders'));
    expect(localStorage.getItem('todayz.biz.store')).toBe('s2');
  });

  it('goes straight on when the remembered store is still there', async () => {
    localStorage.setItem('todayz.biz.store', 's2');
    const { history } = renderAt('/choose-shop?next=/orders', {
      handlers: [shops([{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'manager', stores: [{ id: 's1', name: 'A' }, { id: 's2', name: 'B' }] }])],
    });
    await waitFor(() => expect(history.location.pathname).toBe('/orders'));
  });

  it('lets the person carry on to the overview while the list is unavailable', async () => {
    const { history } = renderAt('/choose-shop');
    fireEvent.click(await screen.findByRole('button', { name: 'Continue to overview' }));
    await waitFor(() => expect(history.location.pathname).toBe('/'));
  });

  it('sends a number with no shop and no invitation straight to shop setup', async () => {
    const { history } = renderAt('/choose-shop', { handlers: [shops([])] });
    await waitFor(() => expect(history.location.pathname).toBe('/onboarding'));
  });
});

describe('sign out and invite', () => {
  it('signs out and returns to sign in', async () => {
    const { session, history } = renderAt('/', {
      handlers: [(c) => (c.path === '/v1/auth/logout' ? new Response(null, { status: 204 }) : undefined)],
    });
    fireEvent.click((await screen.findAllByRole('button', { name: 'Sign out' }))[0] as HTMLElement);
    await waitFor(() => expect(session.getState().status).toBe('signedOut'));
    await waitFor(() => expect(history.location.pathname).toBe('/sign-in'));
  });

  it('keeps an invite link behind sign in and shows the placeholder once signed in', async () => {
    const out = renderAt('/invite/abc123', { signedIn: false });
    await screen.findByRole('heading', { level: 1, name: 'Sign in to TodayZ Business' });
    expect(out.history.location.search).toContain('invite');
    out.unmount();
    renderAt('/invite/abc123');
    expect(await screen.findByRole('heading', { level: 1, name: 'Join a shop' })).toBeInTheDocument();
  });
});

describe('user switch (review 8)', () => {
  it('clears the query cache when a different user signs in', () => {
    const { session, queryClient } = renderAt('/sign-in');
    const jwt = (sub: string) => `${btoa('{}')}.${btoa(JSON.stringify({ sub }))}.s`;
    const exp = new Date(Date.now() + 900_000).toISOString();
    session.signIn({ accessToken: jwt('u1'), expiresAt: exp });
    queryClient.setQueryData(['shops'], [1]);
    session.signIn({ accessToken: jwt('u2'), expiresAt: exp });
    expect(queryClient.getQueryData(['shops'])).toBeUndefined();
  });
});
