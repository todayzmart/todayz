import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt } from './render';

const me = { id: '01J9ZAAAAAAAAAAAAAAAAAAAAA', name: null, preferredLang: 'en', phoneMasked: '90•••••011', app: 'customer', createdAt: '2026-10-01T00:00:00Z' };
const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: me.id });

describe('Account when the first check cannot reach the server', () => {
  it('shows the offline notice and a Try again button instead of a skeleton for ever', async () => {
    let up = false;
    const { calls } = renderAt('/account', {
      returning: true,
      handlers: [
        (c) => (c.path === '/v1/auth/refresh' ? (up ? json(200, grant()) : problem(503, 'unavailable')) : undefined),
        (c) => (c.path === '/v1/me' ? json(200, me, { etag: '"v1"' }) : undefined),
      ],
    });
    expect(await screen.findByText("You're offline.")).toBeInTheDocument();
    const before = calls.filter((c) => c.path === '/v1/auth/refresh').length;
    up = true;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('90•••••011')).toBeInTheDocument();
    expect(calls.filter((c) => c.path === '/v1/auth/refresh').length).toBeGreaterThan(before);
    expect(screen.queryByText("You're offline.")).toBeNull();
  });
});

describe('Account sign out', () => {
  it('clears the whole query cache, not only the account', async () => {
    const { queryClient } = renderAt('/account', {
      returning: true,
      handlers: [
        (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined),
        (c) => (c.path === '/v1/auth/logout' ? new Response(null, { status: 204 }) : undefined),
        (c) => (c.path === '/v1/me' ? json(200, me, { etag: '"v1"' }) : undefined),
      ],
    });
    await screen.findByText('90•••••011');
    queryClient.setQueryData(['orders'], [{ id: 'o1' }]);
    queryClient.setQueryData(['bookings', 'b1'], { id: 'b1' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    });
    await waitFor(() => expect(queryClient.getQueryCache().getAll()).toHaveLength(0));
    expect(queryClient.getQueryData(['orders'])).toBeUndefined();
  });
});
