import type { AdminReferral } from '@todayz/api-client/gen/model/adminReferral';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CUSTOMER_ID, NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateReferralsSearch } from './search';

const REFERRAL_ID = '01JREFERRA00000000000000R1';

function referral(over: Partial<AdminReferral> = {}): AdminReferral {
  return {
    id: REFERRAL_ID,
    kind: 'retailer_customer',
    status: 'qualified',
    source: 'qr_standee',
    code: 'K7QX3M',
    referrer: { type: 'store', id: STORE_ID, label: 'Sri Lakshmi Stores' },
    referee: { type: 'customer', id: CUSTOMER_ID, label: null },
    referrerCredits: 50,
    refereeCredits: 30,
    rejectReason: null,
    qualifiedAt: '2026-10-25T05:00:00Z',
    rewardedAt: null,
    createdAt: '2026-10-24T05:00:00Z',
    openFlags: 1,
    flagRules: ['referral.self_like'],
    ...over,
  };
}

function api(start = referral()): Handler {
  let current = start;
  return (c) => {
    if (c.path === '/v1/admin/referrals' && c.method === 'GET') return json(200, { items: [current], nextCursor: null, serverNow: NOW });
    if (c.path === `/v1/admin/referrals/${REFERRAL_ID}/reject` && c.method === 'POST') {
      if (current.status !== 'pending' && current.status !== 'qualified') return problem(409, 'invalid_transition');
      current = { ...current, status: 'rejected', rejectReason: 'admin_rejected', qualifiedAt: null };
      return json(200, current);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('referrals search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateReferralsSearch({ status: 'rewarded', kind: 'any_shop', flagged: 'true', referral: REFERRAL_ID, x: 1 })).toEqual({
      status: 'rewarded',
      kind: 'any_shop',
      flagged: true,
      referral: REFERRAL_ID,
    });
    expect(validateReferralsSearch({ status: 'paid', kind: 'other', flagged: 'yes', referral: 'x' })).toEqual({});
  });
});

describe('A-10 referrals', () => {
  it('lists referrals and puts the filters in the URL', async () => {
    const { calls, router } = renderAt('/referrals', { roles: ['admin'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Referrals' });
    expect(await within(table).findByText('Sri Lakshmi Stores → Customer')).toBeInTheDocument();
    const form = screen.getByRole('search', { name: 'Referral filters' });
    fireEvent.change(within(form).getByLabelText('Status'), { target: { value: 'qualified' } });
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'qualified' }));
    fireEvent.click(within(form).getByLabelText('Only with open risk flags'));
    await waitFor(() => expect(router.state.location.search).toEqual({ status: 'qualified', flagged: true }));
    await waitFor(() => {
      const q = new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/referrals').at(-1)?.search);
      expect(q.get('status')).toBe('qualified');
      expect(q.get('flagged')).toBe('true');
    });
  });

  it('rejects an unpaid referral with a required, trimmed reason', async () => {
    const { calls, router } = renderAt('/referrals', { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'K7QX3M' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ referral: REFERRAL_ID }));
    expect(await screen.findByRole('heading', { name: 'Referral K7QX3M' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Referral looks like self-referral' })).toHaveAttribute('href', '/risk?rule=referral.self_like');
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Reject' }));
    const dialog = await screen.findByRole('dialog', { name: 'Reject referral K7QX3M?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    expect(await within(dialog).findByText('Give a reason.')).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/reject'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: '  owner referred own phone  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    await waitFor(() => expect(screen.getByText('Referral rejected')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/referrals/${REFERRAL_ID}/reject`)?.body).toEqual({ reason: 'owner referred own phone' });
    expect(await screen.findByText('admin_rejected')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject' })).not.toBeInTheDocument();
  });

  it('offers no reject once the reward posted', async () => {
    renderAt(`/referrals?referral=${REFERRAL_ID}`, { roles: ['super_admin'], handlers: [api(referral({ status: 'rewarded', rewardedAt: NOW }))] });
    expect(await screen.findByRole('heading', { name: 'Referral K7QX3M' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject' })).not.toBeInTheDocument();
  });

  it('shows the 409 when the reward posted in the meantime', async () => {
    const handler: Handler = (c) => {
      if (c.path === `/v1/admin/referrals/${REFERRAL_ID}/reject`) return problem(409, 'invalid_transition');
      return api()(c);
    };
    renderAt(`/referrals?referral=${REFERRAL_ID}`, { roles: ['admin'], handlers: [handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reject' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'abuse' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });
});
