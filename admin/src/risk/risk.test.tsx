import type { RiskFlag } from '@todayz/api-client/gen/model/riskFlag';
import type { RiskFlagDetail } from '@todayz/api-client/gen/model/riskFlagDetail';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CUSTOMER_ID, NOW, ORDER_ID, STORE_ID, customer, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateRiskSearch } from './search';

const FLAG_ID = '01JF1AG0000000000000000F01';
const OTHER_ID = '01JF1AG0000000000000000F02';
const REFERRAL_ID = '01JREFERRA00000000000000R1';

function flag(over: Partial<RiskFlag> = {}): RiskFlag {
  return {
    id: FLAG_ID,
    rule: 'referral.self_like',
    subjectType: 'referral',
    subjectId: REFERRAL_ID,
    subjectLabel: 'Sri Lakshmi Stores → Asha',
    score: 70,
    status: 'open',
    createdAt: '2026-10-25T06:00:00Z',
    reviewedAt: null,
    reviewedBy: null,
    reviewNote: null,
    ...over,
  };
}

function detail(f: RiskFlag, over: Partial<RiskFlagDetail> = {}): RiskFlagDetail {
  return {
    flag: f,
    subject: {
      type: 'referral',
      id: REFERRAL_ID,
      label: 'Sri Lakshmi Stores → Asha',
      status: 'qualified',
      storeId: STORE_ID,
      businessId: null,
      customerId: CUSTOMER_ID,
      customerParty: 'referee',
      businessParty: null,
    },
    facts: [
      { key: 'minutes_to_first_order', value: '4' },
      { key: 'shared_device', value: 'true' },
    ],
    evidence: [
      { kind: 'order', id: ORDER_ID, label: 'First order', storeName: 'Sri Lakshmi Stores', at: '2026-10-25T05:50:00Z', amountPaise: 12000, note: null },
    ],
    history: [flag({ id: OTHER_ID, rule: 'referral.device_accounts', status: 'cleared' })],
    assistEnabled: false,
    serverNow: NOW,
    ...over,
  };
}

function api(start = flag(), opts: { assist?: boolean } = {}): Handler {
  let current = start;
  return (c) => {
    if (c.path === '/v1/admin/risk/flags' && c.method === 'GET') {
      const q = new URLSearchParams(c.search);
      const items = [current].filter((f) => f.status === (q.get('status') ?? 'open') && (!q.get('rule') || f.rule === q.get('rule')));
      return json(200, {
        items,
        openByRule: current.status === 'open' ? [{ rule: current.rule, open: 1 }] : [],
        nextCursor: null,
        serverNow: NOW,
      });
    }
    if (c.path === `/v1/admin/risk/flags/${FLAG_ID}` && c.method === 'GET') return json(200, detail(current, { assistEnabled: !!opts.assist }));
    for (const verdict of ['clear', 'confirm'] as const) {
      if (c.path === `/v1/admin/risk/flags/${FLAG_ID}/${verdict}` && c.method === 'POST') {
        const { from } = c.body as { from: string };
        const ok = from === current.status && (current.status === 'open' || (current.status === 'confirmed' && verdict === 'clear'));
        if (!ok) return problem(409, 'invalid_transition');
        current = {
          ...current,
          status: verdict === 'clear' ? 'cleared' : 'confirmed',
          reviewedAt: NOW,
          reviewedBy: 'Ravi',
          reviewNote: (c.body as { reason: string }).reason,
        };
        return json(200, current);
      }
    }
    if (c.path === `/v1/admin/customers/${CUSTOMER_ID}/block` && c.method === 'POST') return json(200, customer({ status: 'blocked' }));
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('risk search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateRiskSearch({ status: 'cleared', rule: 'pos.reversal_ratio', flag: FLAG_ID, x: 1 })).toEqual({
      status: 'cleared',
      rule: 'pos.reversal_ratio',
      flag: FLAG_ID,
    });
    expect(validateRiskSearch({ status: 'deleted', rule: 'Bad Rule!', flag: 'nope' })).toEqual({});
  });
});

describe('A-13 risk', () => {
  it('lists open flags by rule and keeps the rule and flag in the URL', async () => {
    const { calls, router } = renderAt('/risk', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Risk flags' });
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/risk/flags')?.search).get('status')).toBe('open');
    fireEvent.click(await screen.findByRole('tab', { name: /Referral looks like self-referral/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ rule: 'referral.self_like' }));
    await waitFor(() => expect(new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/risk/flags').at(-1)?.search).get('rule')).toBe('referral.self_like'));
    fireEvent.click(await within(table).findByRole('button', { name: 'Referral looks like self-referral' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ rule: 'referral.self_like', flag: FLAG_ID }));
    expect(await screen.findByRole('heading', { name: 'Referral looks like self-referral: Sri Lakshmi Stores → Asha' })).toBeInTheDocument();
    expect(screen.getByText('minutes_to_first_order')).toBeInTheDocument();
    expect(within(screen.getByRole('table', { name: 'Evidence' })).getByText('First order')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'One device, many referral accounts' })).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();
  });

  it('is read-only for support', async () => {
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['support'], handlers: [api()] });
    expect(await screen.findByRole('heading', { name: /Sri Lakshmi Stores → Asha/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clear' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();
  });

  it('shows support no follow-up on a confirmed flag', async () => {
    renderAt(`/risk?status=confirmed&flag=${FLAG_ID}`, { roles: ['support'], handlers: [api(flag({ status: 'confirmed', reviewedBy: 'Ravi', reviewNote: 'abuse' }))] });
    expect(await screen.findByText('By Ravi: abuse')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Block customer' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Suspend business' })).not.toBeInTheDocument();
  });

  it('clears with a required, trimmed reason', async () => {
    const { calls } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Clear' }));
    const dialog = await screen.findByRole('dialog', { name: /^Clear / });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear' }));
    expect(await within(dialog).findByText('Give a reason.')).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/clear'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: '  same family, real customer  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(screen.getByText('Flag cleared')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/risk/flags/${FLAG_ID}/clear`)?.body).toEqual({ from: 'open', reason: 'same family, real customer' });
    expect(await screen.findByText('By Ravi: same family, real customer')).toBeInTheDocument();
  });

  it('warns that clearing a held reward lets it post', async () => {
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api(flag({ rule: 'referral.reward_refused' }))] });
    fireEvent.click(await screen.findByRole('button', { name: 'Clear' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/held referral reward post on the next sweep/)).toBeInTheDocument();
  });

  it('confirms, then offers the customer block as its own audited call', async () => {
    const { calls } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    let dialog = await screen.findByRole('dialog', { name: /^Confirm / });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'same device and address' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(screen.getByText('Flag confirmed')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/risk/flags/${FLAG_ID}/confirm`)?.body).toEqual({ from: 'open', reason: 'same device and address' });
    // The confirm alone blocked no one.
    expect(calls.some((c) => c.path.endsWith('/block'))).toBe(false);
    expect(screen.queryByRole('button', { name: 'Suspend business' })).not.toBeInTheDocument();

    // Confirmed: still clearable, no longer confirmable.
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();

    fireEvent.click(await screen.findByRole('button', { name: 'Block customer' }));
    dialog = await screen.findByRole('dialog', { name: 'Block the referee?' });
    expect(within(dialog).getByText(/^The referee is signed out/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'confirmed referral abuse' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Block customer' }));
    await waitFor(() => expect(screen.getByText('Customer blocked')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/customers/${CUSTOMER_ID}/block`)?.body).toEqual({ reason: 'confirmed referral abuse' });
  });

  it('clears a confirmed flag from confirmed', async () => {
    const { calls } = renderAt(`/risk?status=confirmed&flag=${FLAG_ID}`, {
      roles: ['admin'],
      handlers: [api(flag({ status: 'confirmed', reviewedBy: 'Ravi', reviewNote: 'abuse' }))],
    });
    expect(await screen.findByText('By Ravi: abuse')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    const dialog = await screen.findByRole('dialog', { name: /^Clear / });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'refund settled, real shop' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(screen.getByText('Flag cleared')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/risk/flags/${FLAG_ID}/clear`)?.body).toEqual({ from: 'confirmed', reason: 'refund settled, real shop' });
  });

  it('notes the payout hold on open and confirmed store and business flags only', async () => {
    const store = flag({ rule: 'pos.reversal_ratio', subjectType: 'store', subjectId: STORE_ID, subjectLabel: 'Sri Lakshmi Stores' });
    const { unmount } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['support'], handlers: [api(store)] });
    expect(await screen.findByText(/^Holds payout/)).toBeInTheDocument();
    unmount();
    const held = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api({ ...store, status: 'confirmed' })] });
    expect(await screen.findByText(/^Holds payout/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(within(await screen.findByRole('dialog')).getByText(/payout is released once no other flag holds it/)).toBeInTheDocument();
    held.unmount();
    renderAt(`/risk?status=cleared&flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api({ ...store, status: 'cleared' })] });
    expect(await screen.findByRole('heading', { name: /Sri Lakshmi Stores/ })).toBeInTheDocument();
    expect(screen.queryByText(/^Holds payout/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clear' })).not.toBeInTheDocument();
  });

  it('names a referral flag after the hold and shows no payout note', async () => {
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api()] });
    await screen.findByRole('heading', { name: /Sri Lakshmi Stores → Asha/ });
    expect(screen.queryByText(/^Holds payout/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(within(await screen.findByRole('dialog')).getByText(/stays on the record until someone clears it/)).toBeInTheDocument();
  });

  it('shows the 409 when the flag was resolved elsewhere', async () => {
    const handler: Handler = (c) => {
      if (c.path === `/v1/admin/risk/flags/${FLAG_ID}/confirm`) return problem(409, 'invalid_transition');
      return api()(c);
    };
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['super_admin'], handlers: [handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'abuse' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });

  it('mounts the assist slot only while the explainer is on', async () => {
    const { unmount } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api()] });
    await screen.findByRole('heading', { name: /Sri Lakshmi Stores → Asha/ });
    expect(document.querySelector('[data-slot="risk-assist"]')).toBeNull();
    unmount();
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [api(flag(), { assist: true })] });
    await screen.findByRole('heading', { name: /Sri Lakshmi Stores → Asha/ });
    expect(document.querySelector(`[data-slot="risk-assist"][data-flag="${FLAG_ID}"]`)).not.toBeNull();
  });
});
