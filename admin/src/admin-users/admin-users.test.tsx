import type { AdminUser } from '@todayz/api-client/gen/model/adminUser';
import type { AdminUserChange } from '@todayz/api-client/gen/model/adminUserChange';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, emptyAudit } from '../test/fixtures';
import { adminMe, json, problem, renderAt, type Handler } from '../test/render';
import { passStepUp, stepUpHandler } from '../test/step-up';
import { phoneOk } from './labels';
import { validateAdminUsersSearch } from './search';

const SELF_ID = adminMe([]).id;
const MEERA_ID = '01JADM1N0000000000000000M1';

function account(over: Partial<AdminUser> = {}): AdminUser {
  return {
    id: MEERA_ID,
    name: 'Meera',
    phoneMasked: '90•••••311',
    roles: ['support'],
    status: 'active',
    createdBy: SELF_ID,
    createdByName: 'Asha Rao',
    createdAt: '2026-10-20T09:30:00Z',
    disabledAt: null,
    lastActiveAt: '2026-10-25T10:12:00Z',
    signInLockedUntil: null,
    version: 1,
    ...over,
  };
}

const self = account({ id: SELF_ID, name: 'Asha Rao', phoneMasked: '90•••••001', roles: ['super_admin'], createdBy: null, createdByName: null });

interface Api {
  handler: Handler;
  stepUpNeeded: boolean;
}

function api(opts: { start?: AdminUser; stepUp?: boolean } = {}): Api {
  let current = opts.start ?? account();
  const state: Api = { stepUpNeeded: opts.stepUp ?? false, handler: () => undefined };
  state.handler = (c) => {
    if (c.path === '/v1/admin/users' && c.method === 'GET') {
      const want = new URLSearchParams(c.search).get('status') ?? 'active';
      const items = [current, self].filter((a) => a.status === want);
      return json(200, { items, nextCursor: null, serverNow: NOW });
    }
    if (c.method !== 'GET' && c.path.startsWith('/v1/admin/users') && state.stepUpNeeded) return problem(403, 'step_up_required');
    if (c.path === '/v1/admin/users' && c.method === 'POST') {
      const b = c.body as { name: string; roles: AdminUser['roles'] };
      return json(201, account({ id: '01JADM1N0000000000000000N2', name: b.name, roles: b.roles, lastActiveAt: null }));
    }
    if (c.path === `/v1/admin/users/${MEERA_ID}` && c.method === 'PATCH') {
      const b = c.body as AdminUserChange;
      if (b.version !== current.version) return problem(409, 'conflict');
      current = {
        ...current,
        ...(b.roles ? { roles: b.roles } : {}),
        ...(b.status ? { status: b.status, disabledAt: b.status === 'disabled' ? NOW : null } : {}),
        version: current.version + 1,
      };
      return json(200, current);
    }
    if (c.path === `/v1/admin/users/${MEERA_ID}/unlock-sign-in` && c.method === 'POST') {
      if (!current.signInLockedUntil) return problem(409, 'invalid_transition');
      current = { ...current, signInLockedUntil: null };
      return json(200, current);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
  return state;
}

/** A toast: the list refetch after a change re-renders the toast region, so query it afresh each try. */
async function sees(text: string) {
  await waitFor(() => expect(screen.getByText(text)).toBeInTheDocument());
}

describe('admin users search and phone', () => {
  it('keeps only well-formed filters', () => {
    expect(validateAdminUsersSearch({ status: 'disabled', admin: MEERA_ID.toLowerCase(), x: 1 })).toEqual({ status: 'disabled', admin: MEERA_ID });
    expect(validateAdminUsersSearch({ status: 'active', admin: 'nope' })).toEqual({});
  });

  it.each([
    ['90000 00311', true],
    ['+91 90000 00311', true],
    ['919000000311', true],
    ['12345', false],
    ['9000000311x', false],
  ])('reads %s as a mobile: %s', (v, ok) => expect(phoneOk(v)).toBe(ok));
});

describe('A-18 admin users', () => {
  it('lists active accounts with masked phones and keeps the chosen one in the URL', async () => {
    const { calls, router } = renderAt('/admin-users', { roles: ['super_admin'], handlers: [api().handler] });
    const table = await screen.findByRole('table', { name: 'Active' });
    expect(await within(table).findByText('90•••••311')).toBeInTheDocument();
    expect(within(table).getByText('Support')).toBeInTheDocument();
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/users')?.search).get('status')).toBe('active');

    fireEvent.click(within(table).getByRole('button', { name: 'Meera' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ admin: MEERA_ID }));
    expect(await screen.findByRole('heading', { name: 'Meera' })).toBeInTheDocument();
    expect(screen.getByText(/^By Asha Rao, 20 Oct, /)).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('tab', { name: 'Disabled' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'disabled' }));
    expect(await screen.findByText('No disabled accounts.')).toBeInTheDocument();
  });

  it('offers no moves on your own account', async () => {
    renderAt(`/admin-users?admin=${SELF_ID}`, { roles: ['super_admin'], handlers: [api().handler] });
    expect(await screen.findByText('This is your account. Another super admin changes your access.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Disable' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Change roles' })).not.toBeInTheDocument();
  });

  it('invites by phone with roles and a reason; the number goes only in the body', async () => {
    const { calls, router } = renderAt('/admin-users', { roles: ['super_admin'], handlers: [api().handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Invite admin' }));
    const dialog = await screen.findByRole('dialog', { name: 'Invite an admin' });
    fireEvent.change(within(dialog).getByLabelText('Mobile number'), { target: { value: '12345' } });
    expect(within(dialog).getByText('Enter a 10-digit mobile number.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Mobile number'), { target: { value: '90000 00412' } });
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: ' Ravi ' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'joins catalogue' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Invite admin' }));
    expect(within(dialog).getByText('Pick at least one role.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST' && c.path === '/v1/admin/users')).toBe(false);

    fireEvent.click(within(dialog).getByLabelText('Finance'));
    fireEvent.click(within(dialog).getByLabelText('Catalogue'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Invite admin' }));
    await sees('Ravi can now sign in to the console.');
    const post = calls.find((c) => c.method === 'POST' && c.path === '/v1/admin/users');
    expect(post?.body).toEqual({ phone: '90000 00412', name: 'Ravi', roles: ['catalogue', 'finance'], reason: 'joins catalogue' });
    expect(calls.every((c) => !c.search.includes('00412'))).toBe(true);
    await waitFor(() => expect(router.state.location.search).toEqual({ admin: '01JADM1N0000000000000000N2' }));
  });

  it('says so when the number already has admin access', async () => {
    const handler: Handler = (c) => (c.method === 'POST' && c.path === '/v1/admin/users' ? problem(409, 'conflict') : api().handler(c));
    renderAt('/admin-users', { roles: ['super_admin'], handlers: [handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Invite admin' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Mobile number'), { target: { value: '9000000311' } });
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Meera' } });
    fireEvent.click(within(dialog).getByLabelText('Support'));
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'again' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Invite admin' }));
    expect(await within(dialog).findByText('This number already has admin access.')).toBeInTheDocument();
  });

  it('asks for a fresh code, then changes roles with the version it read', async () => {
    const a = api({ stepUp: true });
    const { calls } = renderAt(`/admin-users?admin=${MEERA_ID}`, { roles: ['super_admin'], handlers: [a.handler, stepUpHandler(a)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Change roles' }));
    const dialog = await screen.findByRole('dialog', { name: 'Change roles for Meera' });
    expect(within(dialog).getByText('These are the roles they have now.')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByLabelText('Catalogue'));
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'reviews suggestions too' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Change roles' }));

    await passStepUp();
    await sees('Roles changed for Meera.');
    const patches = calls.filter((c) => c.method === 'PATCH');
    expect(patches).toHaveLength(2);
    expect(patches[1]?.body).toEqual({ roles: ['support', 'catalogue'], version: 1, reason: 'reviews suggestions too' });
  });

  it('disables with a reason and shows the stale-version conflict', async () => {
    const stale: Handler = (c) => (c.method === 'PATCH' ? problem(409, 'conflict') : undefined);
    const { calls } = renderAt(`/admin-users?admin=${MEERA_ID}`, { roles: ['super_admin'], handlers: [stale, api().handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Disable' }));
    const dialog = await screen.findByRole('dialog', { name: 'Disable Meera?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disable' }));
    expect(await within(dialog).findByText(/Write a reason/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'left the team' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Disable' }));
    expect(await within(dialog).findByText('Someone changed this account a moment ago. Close this, check it again, then retry.')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ status: 'disabled', version: 1, reason: 'left the team' });
  });

  it('re-enables a disabled account and offers no authenticator reset (D-264)', async () => {
    const { calls } = renderAt(`/admin-users?status=disabled&admin=${MEERA_ID}`, {
      roles: ['super_admin'],
      handlers: [api({ start: account({ status: 'disabled', disabledAt: NOW }) }).handler],
    });
    expect(await screen.findByRole('heading', { name: 'Meera' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Change roles' })).not.toBeInTheDocument();

    expect(screen.queryByRole('button', { name: /authenticator/i })).not.toBeInTheDocument();
    expect(screen.queryByText('Authenticator')).not.toBeInTheDocument();

    fireEvent.click(await screen.findByRole('button', { name: 'Enable' }));
    const dialog = await screen.findByRole('dialog', { name: 'Enable Meera?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'back from leave' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Enable' }));
    await sees('Meera can sign in again.');
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ status: 'active', version: 1, reason: 'back from leave' });
  });

  it('offers "Unlock sign-in" only while the sign-in is locked (D-264)', async () => {
    renderAt(`/admin-users?admin=${MEERA_ID}`, { roles: ['super_admin'], handlers: [api().handler] });
    expect(await screen.findByRole('heading', { name: 'Meera' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Unlock sign-in' })).not.toBeInTheDocument();
    expect(screen.queryByText('Locked')).not.toBeInTheDocument();
  });

  it('unlocks a locked sign-in after a fresh code, with a reason (D-264)', async () => {
    const a = api({ start: account({ signInLockedUntil: '2026-10-28T06:00:00Z' }), stepUp: true });
    const { calls } = renderAt(`/admin-users?admin=${MEERA_ID}`, { roles: ['super_admin'], handlers: [a.handler, stepUpHandler(a)] });
    expect(await screen.findByText('Locked')).toBeInTheDocument();
    expect(screen.getByText(/^until /)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Unlock sign-in' }));
    const dialog = await screen.findByRole('dialog', { name: 'Unlock sign-in for Meera?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Unlock sign-in' }));
    expect(await within(dialog).findByText(/Write a reason/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'called her back on the office line' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Unlock sign-in' }));

    await passStepUp();
    await sees('Meera can sign in again.');
    const posts = calls.filter((c) => c.method === 'POST' && c.path === `/v1/admin/users/${MEERA_ID}/unlock-sign-in`);
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body).toEqual({ reason: 'called her back on the office line' });
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Unlock sign-in' })).not.toBeInTheDocument());
    expect(screen.queryByText('Locked')).not.toBeInTheDocument();
  });

  it('says so when the lock has already gone (D-264)', async () => {
    const gone: Handler = (c) => (c.method === 'POST' && c.path.endsWith('/unlock-sign-in') ? problem(409, 'invalid_transition') : undefined);
    renderAt(`/admin-users?admin=${MEERA_ID}`, {
      roles: ['super_admin'],
      handlers: [gone, api({ start: account({ signInLockedUntil: '2026-10-28T06:00:00Z' }) }).handler],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Unlock sign-in' }));
    const dialog = await screen.findByRole('dialog', { name: 'Unlock sign-in for Meera?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'checked' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Unlock sign-in' }));
    expect(await within(dialog).findByText(/their sign-in is not locked any more/)).toBeInTheDocument();
  });

  it('never offers to unlock your own sign-in', async () => {
    const lockedSelf = { ...self, signInLockedUntil: '2026-10-28T06:00:00Z' };
    const h: Handler = (c) =>
      c.path === '/v1/admin/users' && c.method === 'GET' ? json(200, { items: [account(), lockedSelf], nextCursor: null, serverNow: NOW }) : undefined;
    renderAt(`/admin-users?admin=${SELF_ID}`, { roles: ['super_admin'], handlers: [h, api().handler] });
    expect(await screen.findByText('This is your account. Another super admin changes your access.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Unlock sign-in' })).not.toBeInTheDocument();
  });
});
