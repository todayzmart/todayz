import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { json, problem, renderAt } from './render';

const HINT = 'todayz.hasSession.business';

describe('B-01 gate when the first check cannot reach the server', () => {
  afterEach(() => localStorage.removeItem(HINT));

  it('shows the offline notice and a Try again button instead of a skeleton for ever', async () => {
    localStorage.setItem(HINT, '1');
    let up = false;
    const grant = {
      accessToken: 'tok',
      expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
      tokenType: 'Bearer',
      userId: '01J9ZAAAAAAAAAAAAAAAAAAAAA',
      isNewUser: false,
    };
    const { calls } = renderAt('/', {
      signedIn: false,
      handlers: [
        (c) => (c.path === '/v1/auth/refresh' ? (up ? json(200, grant) : problem(503, 'unavailable')) : undefined),
        (c) => (c.path === '/v1/biz/businesses' ? json(200, { items: [], invitations: [] }) : undefined),
      ],
    });
    expect(await screen.findByText("You're offline.")).toBeInTheDocument();
    const before = calls.filter((c) => c.path === '/v1/auth/refresh').length;
    up = true;
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(screen.queryByText("You're offline.")).toBeNull());
    expect(calls.filter((c) => c.path === '/v1/auth/refresh').length).toBeGreaterThan(before);
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
  });
});
