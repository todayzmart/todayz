import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NAV } from '../../shell/nav';
import { renderAt } from '../../test/render';
import { ROLES, SECTION_ROLES, can, canOpen } from './access';

const sidebar = async () => within(await screen.findByRole('navigation', { name: 'Main' }));

describe('role-aware sidebar (D-64)', () => {
  it.each(ROLES)('%s sees exactly the sections the role table opens', async (role) => {
    renderAt('/settings', { roles: [role] });
    const side = await sidebar();
    const want = NAV.filter((n) => SECTION_ROLES[n.section]?.includes(role)).map((n) => (n.section === '' ? '/' : `/${n.section}`));
    await screen.findAllByRole('link');
    await expect.poll(() => side.queryAllByRole('link').length).toBe(want.length);
    expect(side.getAllByRole('link').map((l) => l.getAttribute('href'))).toEqual(want);
  });

  it('every nav section has a role entry', () => {
    for (const n of NAV) expect(SECTION_ROLES[n.section], n.section).toBeDefined();
  });

  it('a section the roles do not open shows no access, naming the roles', async () => {
    renderAt('/settings', { roles: ['support', 'finance'] });
    expect(await screen.findByRole('heading', { name: "You don't have access to this screen" })).toBeInTheDocument();
    expect(screen.getByText(/Your roles: Support, Finance\./)).toBeInTheDocument();
  });

  it('a field agent starts at A-04 Add shops (Pulse is not theirs) and sees no access on a hidden screen', async () => {
    const { router, unmount } = renderAt('/', { roles: ['field_agent'] });
    await waitFor(() => expect(router.state.location.pathname).toBe('/field'));
    const side = await sidebar();
    expect(side.getAllByRole('link').map((l) => l.textContent)).toEqual(['Add shops', 'Shop checks', 'Businesses', 'Demand', 'Field agents']);
    unmount();

    renderAt('/orders', { roles: ['field_agent'] });
    expect(await screen.findByRole('heading', { name: "You don't have access to this screen" })).toBeInTheDocument();
    expect(screen.getByText('Your roles: Field agent. Ask a super admin if you need this screen.')).toBeInTheDocument();
  });

  it('an account with no role sees nothing to open', async () => {
    renderAt('/', { roles: [] });
    expect(await screen.findByText(/no admin role yet/)).toBeInTheDocument();
    expect((await sidebar()).queryAllByRole('link')).toHaveLength(0);
  });

  it('when the account cannot load, the page offers a retry instead of guessing', async () => {
    renderAt('/customers', { handlers: [(c) => (c.path === '/v1/me' ? new Response('', { status: 503 }) : undefined)] });
    expect(await screen.findByRole('heading', { name: "We couldn't load your access" }, { timeout: 4000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });

  it('unknown pages still show not found', async () => {
    renderAt('/no-such-page', { roles: ['support'] });
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument();
  });

  it('action gates follow the matrix', () => {
    expect(can(['support'], 'strikesReset')).toBe(true);
    expect(can(['support'], 'customerBlock')).toBe(false);
    expect(can(['support'], 'orderCancel')).toBe(true);
    expect(can(['admin'], 'customerBlock')).toBe(true);
    expect(can(['admin'], 'auditLog')).toBe(true);
    expect(can(['finance'], 'orderCancel')).toBe(false);
    expect(canOpen(['admin'], 'admin-users')).toBe(false);
    expect(canOpen(['super_admin'], 'admin-users')).toBe(true);
  });
});
