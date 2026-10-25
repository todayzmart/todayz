import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { auditEntry, emptyAudit } from '../test/fixtures';
import { json, renderAt, type Handler } from '../test/render';
import { changes } from './diff';
import { auditParams, validateAuditSearch } from './search';

describe('audit diff', () => {
  it('lists changed, added and removed fields in key order', () => {
    expect(changes({ status: 'active', strikes: 2, same: 'x' }, { status: 'blocked', reason: 'fake', same: 'x' })).toEqual([
      { key: 'reason', before: null, after: 'fake' },
      { key: 'status', before: 'active', after: 'blocked' },
      { key: 'strikes', before: '2', after: null },
    ]);
    expect(changes(null, null)).toEqual([]);
    expect(changes({ a: { b: 1 } }, { a: { b: 1 } })).toEqual([]);
  });
});

describe('audit search', () => {
  it('drops malformed filters and maps days to IST bounds', () => {
    const s = validateAuditSearch({ action: 'customer.block', subjectType: 'user', subjectId: 'a b', from: '2026-10-25', actorId: 'x' });
    expect(s).toEqual({ action: 'customer.block', subjectType: 'user', from: '2026-10-25' });
    expect(auditParams(s)).toEqual({ action: 'customer.block', subjectType: 'user', from: '2026-10-24T18:30:00.000Z' });
  });

  it("keeps W3-01's money subjects", () => {
    for (const subjectType of ['payout', 'payout_period', 'credit_adjustment'] as const) {
      expect(validateAuditSearch({ subjectType })).toEqual({ subjectType });
    }
  });
});

describe('A-19 audit log', () => {
  const log: Handler = (c) => (c.path === '/v1/admin/audit-log' ? json(200, { ...emptyAudit, items: [auditEntry()] }) : undefined);

  it('opens a row to show its changes', async () => {
    renderAt('/audit', { roles: ['admin'], handlers: [log] });
    const table = await screen.findByRole('table', { name: 'Audit log' });
    fireEvent.click(await within(table).findByRole('button', { name: 'Customer blocked' }));
    const sheet = await screen.findByRole('dialog', { name: 'Customer blocked' });
    expect(within(sheet).getByLabelText('was active')).toHaveTextContent('active');
    expect(within(sheet).getByText('fake orders')).toBeInTheDocument();
  });

  it('names the payout and wallet actions', async () => {
    const money: Handler = (c) =>
      c.path === '/v1/admin/audit-log'
        ? json(200, {
            ...emptyAudit,
            items: [
              auditEntry({ id: '01JAUDIT00000000000000A002', action: 'payouts.mark_paid', subjectType: 'payout', before: null, after: { status: 'paid' } }),
              auditEntry({ id: '01JAUDIT00000000000000A003', action: 'credits.adjustment_request', subjectType: 'credit_adjustment', before: null, after: null }),
            ],
          })
        : undefined;
    renderAt('/audit', { roles: ['admin'], handlers: [money] });
    const table = await screen.findByRole('table', { name: 'Audit log' });
    expect(await within(table).findByRole('button', { name: 'Payout marked paid' })).toBeInTheDocument();
    expect(within(table).getByRole('button', { name: 'Wallet adjustment requested' })).toBeInTheDocument();
  });

  it('puts the filters in the URL', async () => {
    const { router, calls } = renderAt('/audit', { roles: ['admin'], handlers: [log] });
    const form = await screen.findByRole('search', { name: 'Audit log filters' });
    fireEvent.change(within(form).getByLabelText('Subject type'), { target: { value: 'order' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ subjectType: 'order' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/audit-log' && c.search.includes('subjectType=order'))).toBe(true));
  });
});
