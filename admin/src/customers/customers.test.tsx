import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CUSTOMER_ID, auditEntry, customer, emptyAudit } from '../test/fixtures';
import { json, renderAt, type Handler } from '../test/render';
import { mobileDigits } from './phone';

const base = `/v1/admin/customers/${CUSTOMER_ID}`;

/** The customer endpoints, answering like the server: each action changes what the next read sees. */
function api(): Handler {
  let current = customer();
  return (c) => {
    if (c.path === '/v1/admin/customer-search' && c.method === 'POST') {
      const { id, name, phoneMasked, status, activeStrikes, createdAt } = current;
      return json(200, { items: [{ id, name, phoneMasked, status, activeStrikes, createdAt }], serverNow: current.serverNow });
    }
    if (c.path === base && c.method === 'GET') return json(200, current);
    if (c.path === `${base}/block` && c.method === 'POST') {
      current = { ...current, status: 'blocked', sessions: [] };
      return json(200, current);
    }
    if (c.path === `${base}/strikes/reset` && c.method === 'POST') {
      current = { ...current, activeStrikes: 0 };
      return json(200, current);
    }
    if (c.path.startsWith(`${base}/sessions/`) && c.method === 'DELETE') {
      current = { ...current, sessions: [] };
      return new Response(null, { status: 204 });
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

function typeReason(dialog: HTMLElement, text: string) {
  fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: text } });
}

describe('mobileDigits', () => {
  it.each([
    ['98765 43210', '9876543210'],
    ['+91 98765 43210', '9876543210'],
    ['919876543210', '9876543210'],
    ['98765-43210', '9876543210'],
  ])('reads %s', (raw, want) => expect(mobileDigits(raw)).toBe(want));

  it.each(['12345', '5876543210', '98765432100', 'abc'])('refuses %s', (raw) => expect(mobileDigits(raw)).toBeNull());
});

describe('A-06 customers', () => {
  it('searches by phone without putting the number in the URL', async () => {
    const { calls, router } = renderAt('/customers', { roles: ['support'], handlers: [api()] });
    fireEvent.change(await screen.findByLabelText('Phone number'), { target: { value: '+91 98765 43210' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));
    const table = await screen.findByRole('table', { name: 'Search results' });
    expect(within(table).getByRole('link', { name: 'Meera K' })).toHaveAttribute('href', `/customers/${CUSTOMER_ID}`);
    expect(within(table).getByText('98•••••210')).toBeInTheDocument();
    const sent = calls.find((c) => c.path === '/v1/admin/customer-search');
    expect(sent?.body).toEqual({ phone: '9876543210' });
    expect(calls.every((c) => !`${c.path}${c.search}`.includes('9876543210'))).toBe(true);
    expect(router.state.location.href).toBe('/customers');
  });

  it('refuses a malformed number without calling the API', async () => {
    const { calls } = renderAt('/customers', { roles: ['support'], handlers: [api()] });
    fireEvent.change(await screen.findByLabelText('Phone number'), { target: { value: '12345' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Enter a 10-digit Indian mobile number.')).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/customer-search')).toBe(false);
  });

  it('support may reset strikes, with a reason, but not block', async () => {
    const { calls } = renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['support'], handlers: [api()] });
    expect(await screen.findByRole('heading', { level: 1, name: 'Meera K' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Block' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Reset strikes' }));
    const dialog = await screen.findByRole('dialog', { name: 'Reset 2 strikes?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reset strikes' }));
    expect(await within(dialog).findByText('Write a reason, up to 500 characters.')).toBeInTheDocument();
    expect(calls.some((c) => c.path === `${base}/strikes/reset`)).toBe(false);
    typeReason(dialog, 'shop mix-up');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reset strikes' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reset strikes' })).toBeDisabled());
    expect(calls.find((c) => c.path === `${base}/strikes/reset`)?.body).toEqual({ reason: 'shop mix-up' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('Strikes reset')).toBeInTheDocument());
  });

  it('an admin blocks with a reason and the audit panel reloads', async () => {
    let audits = 0;
    const panel: Handler = (c) => {
      if (c.path !== '/v1/admin/audit-log/subject') return undefined;
      audits += 1;
      return json(200, audits > 1 ? { ...emptyAudit, items: [auditEntry()] } : emptyAudit);
    };
    const { calls } = renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['admin'], handlers: [panel, api()] });
    expect(await screen.findByText('No changes yet.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Block' }));
    const dialog = await screen.findByRole('dialog', { name: 'Block Meera K?' });
    typeReason(dialog, 'fake orders');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Block' }));
    expect(await screen.findByRole('button', { name: 'Unblock' })).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/block`)?.body).toEqual({ reason: 'fake orders' });
    const history = screen.getByRole('complementary', { name: 'History' });
    expect(await within(history).findByText('Customer blocked')).toBeInTheDocument();
    expect(within(history).getByLabelText('was active')).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/admin/audit-log/subject')?.search).toContain('subjectType=user');
    expect(screen.getByText('Not signed in anywhere.')).toBeInTheDocument();
  });

  it('an admin signs one device out', async () => {
    const { calls } = renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['admin'], handlers: [api()] });
    const devices = await screen.findByRole('table', { name: 'Signed-in devices' });
    fireEvent.click(within(devices).getByRole('button', { name: 'Sign out' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Sign out this device?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByText('Not signed in anywhere.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'DELETE' && c.path === `${base}/sessions/01JSESS0000000000000000E01`)).toBe(true);
  });

  it('an admin account offers no block, strike reset or sign-out', async () => {
    const admin = customer({ isAdmin: true, sessions: [] });
    renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['super_admin'], handlers: [(c) => (c.path === base ? json(200, admin) : undefined), api()] });
    expect(await screen.findByText(/This is an admin account/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Block' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reset strikes' })).not.toBeInTheDocument();
    expect(screen.getByText("An admin account's devices are not shown here.")).toBeInTheDocument();
  });

  it('a staff-only account cannot be blocked here', async () => {
    const staff = customer({ isCustomer: false });
    renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['admin'], handlers: [(c) => (c.path === base ? json(200, staff) : undefined), api()] });
    expect(await screen.findByText(/no customer profile/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Block' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Reset strikes' })).toBeInTheDocument();
  });

  it('a missing customer says so', async () => {
    renderAt(`/customers/${CUSTOMER_ID}`, { roles: ['support'] });
    expect(await screen.findByRole('heading', { name: "This customer doesn't exist" })).toBeInTheDocument();
  });
});
