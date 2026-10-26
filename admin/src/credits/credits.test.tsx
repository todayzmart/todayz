import type { AdminLedgerAccount } from '@todayz/api-client/gen/model/adminLedgerAccount';
import type { CreditAdjustmentRequest } from '@todayz/api-client/gen/model/creditAdjustmentRequest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateCreditsSearch } from './search';

const ACC = '01JB0AA1A2B3C4D5E6F7G8H9J0';
const REQ = '01JB0AR1A2B3C4D5E6F7G8H9J0';
const MINE = '01JB0AR2A2B3C4D5E6F7G8H9J0';
const KEY = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const PHONE = '9876543210';

const wallet: AdminLedgerAccount = {
  id: ACC,
  kind: 'customer_wallet',
  ownerType: 'customer',
  ownerId: '01JB0CW1A2B3C4D5E6F7G8H9J0',
  ownerName: 'Priya',
  balancePaise: 120000,
};

function request(over: Partial<CreditAdjustmentRequest> = {}): CreditAdjustmentRequest {
  return {
    id: REQ,
    customerId: '01JB0CW1A2B3C4D5E6F7G8H9J0',
    customerName: 'Priya',
    amountPaise: 80000,
    reason: 'Goodwill after a failed delivery',
    link: 'TZ-024817',
    status: 'pending',
    requestedBy: '01JB0ADM1A2B3C4D5E6F7G8H9J',
    requestedByName: 'Ravi',
    requestedAt: '2026-10-25T05:00:00Z',
    mine: false,
    approvedBy: null,
    approvedByName: null,
    decidedAt: null,
    decisionNote: null,
    transactionId: null,
    ...over,
  };
}

interface Api {
  handler: Handler;
  stepUpNeeded: boolean;
  /** What the create answers: posted at once, or pending above the threshold. */
  outcome: 'posted' | 'pending';
}

function api(opts: { stepUp?: boolean; outcome?: 'posted' | 'pending' } = {}): Api {
  const state: Api = { stepUpNeeded: opts.stepUp ?? false, outcome: opts.outcome ?? 'posted', handler: () => undefined };
  state.handler = (c) => {
    if (c.path === '/v1/admin/ledger/accounts' && c.method === 'GET') return json(200, { items: [wallet], serverNow: NOW });
    if (c.path === `/v1/admin/ledger/accounts/${ACC}/entries` && c.method === 'GET') {
      return json(200, {
        account: wallet,
        items: [
          {
            id: '01JB0LE1A2B3C4D5E6F7G8H9J0',
            transactionId: '01JB0TT1A2B3C4D5E6F7G8H9J0',
            kind: 'cashback',
            refType: 'order',
            refId: '01JB0OR1A2B3C4D5E6F7G8H9J0',
            memo: null,
            amountPaise: 12000,
            createdBy: null,
            createdAt: '2026-10-24T06:00:00Z',
          },
        ],
        nextCursor: null,
        serverNow: NOW,
      });
    }
    if (c.path === '/v1/admin/credits/expiry-preview' && c.method === 'GET') {
      return json(200, {
        from: NOW,
        to: '2026-11-01T18:30:00Z',
        totalPaise: 450000,
        days: [{ day: '2026-10-28', expiringPaise: 450000, lots: 9, customers: 7 }],
        serverNow: NOW,
      });
    }
    if (c.path === '/v1/admin/credit-adjustments' && c.method === 'GET') {
      return json(200, { items: [request(), request({ id: MINE, mine: true, customerName: 'Kiran', amountPaise: -60000 })], nextCursor: null, serverNow: NOW });
    }
    if (c.path.startsWith('/v1/admin/credit-adjustments') && c.method === 'POST') {
      if (state.stepUpNeeded) return problem(403, 'step_up_required');
      if (c.path === '/v1/admin/credit-adjustments') {
        const pending = state.outcome === 'pending';
        return json(201, { outcome: state.outcome, ...(pending ? { request: request() } : {}), transactionId: pending ? null : '01JB0TT9A2B3C4D5E6F7G8H9J0', serverNow: NOW });
      }
      if (c.path.endsWith('/approve')) {
        if (c.path.includes(MINE)) return problem(403, 'approver_is_requester');
        return json(200, { outcome: 'approved', request: request({ status: 'approved' }), transactionId: '01JB0TT2A2B3C4D5E6F7G8H9J0', serverNow: NOW });
      }
      if (c.path.endsWith('/reject')) {
        const mine = c.path.includes(MINE);
        return json(200, { outcome: mine ? 'cancelled' : 'rejected', request: request({ status: mine ? 'cancelled' : 'rejected' }), transactionId: null, serverNow: NOW });
      }
    }
    return undefined;
  };
  return state;
}

function stepUpVerify(state: Api): Handler {
  return (c) => {
    if (c.path !== '/v1/auth/totp/verify') return undefined;
    state.stepUpNeeded = false;
    return json(200, { accessToken: 'stepped-up', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer' });
  };
}

async function typeCode(code: string) {
  const boxes = (await screen.findByRole('group', { name: 'Authenticator code' })).querySelectorAll('input');
  for (const [i, d] of code.split('').entries()) fireEvent.change(boxes[i] as HTMLInputElement, { target: { value: d } });
}

/** A toast moves out of a closing dialog's top layer into the page, so look it up each try. */
async function toastSays(text: string) {
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(text));
}

describe('credits search', () => {
  it('keeps the tab, an account id and a known status', () => {
    expect(validateCreditsSearch({ tab: 'approvals', account: ACC.toLowerCase(), status: 'all', q: PHONE })).toEqual({ tab: 'approvals', account: ACC, status: 'all' });
    expect(validateCreditsSearch({ tab: 'x', account: 'nope', status: 'lost' })).toEqual({});
  });
});

describe('A-09 credits', () => {
  it('finds an account by phone without putting the number in the URL, then shows its ledger lines', async () => {
    const { calls, history } = renderAt('/credits', { roles: ['finance'], handlers: [api().handler] });
    expect(await screen.findByRole('heading', { name: 'Expiring soon: ₹4,500' })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Phone, store code or id'), { target: { value: PHONE } });
    fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    const results = await screen.findByRole('table', { name: 'Accounts' });
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/ledger/accounts')?.search).get('q')).toBe(PHONE);
    fireEvent.click(within(results).getByRole('button', { name: 'Priya' }));
    const panel = await screen.findByRole('complementary', { name: 'Priya' });
    expect(within(panel).getByRole('table', { name: 'Ledger lines' })).toHaveTextContent('Cashback');
    expect(within(panel).getByText('₹1,200')).toBeInTheDocument();
    await waitFor(() => expect(history.location.search).toContain(`account=${ACC}`));
    expect(history.location.href).not.toContain(PHONE);
  });

  it('lets the admin role read without the adjust form or decisions', async () => {
    renderAt(`/credits?account=${ACC}`, { roles: ['admin'], handlers: [api().handler] });
    const panel = await screen.findByRole('complementary', { name: 'Priya' });
    expect(within(panel).queryByRole('button', { name: 'Adjust credits' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'Pending approvals' }));
    const table = await screen.findByRole('table', { name: 'Adjustment requests' });
    expect(within(table).queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
    expect(await within(table).findAllByText('Waiting for finance')).toHaveLength(2);
  });

  it('adjusts after a step-up with whole credits, a reason, the link and an Idempotency-Key', async () => {
    const a = api({ stepUp: true });
    const { calls } = renderAt(`/credits?account=${ACC}`, { roles: ['finance'], handlers: [a.handler, stepUpVerify(a)] });
    const panel = await screen.findByRole('complementary', { name: 'Priya' });
    fireEvent.click(within(panel).getByRole('button', { name: 'Adjust credits' }));
    const dialog = await screen.findByRole('dialog', { name: 'Adjust credits for Priya' });
    fireEvent.change(within(dialog).getByLabelText('Credits'), { target: { value: '2.5' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adjust' }));
    expect(await within(dialog).findByText('Enter whole credits from 1 to 10,00,000.')).toBeInTheDocument();
    expect(within(dialog).getByText('Write a reason, up to 500 characters.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST' && c.path === '/v1/admin/credit-adjustments')).toBe(false);

    fireEvent.change(within(dialog).getByLabelText('Change'), { target: { value: 'remove' } });
    fireEvent.change(within(dialog).getByLabelText('Credits'), { target: { value: '300' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Duplicate cashback' } });
    fireEvent.change(within(dialog).getByLabelText('Ticket or order (optional)'), { target: { value: 'TZ-1' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adjust' }));
    await screen.findByRole('dialog', { name: 'Confirm it is you' });
    await typeCode('123456');
    await toastSays("-₹300 posted to Priya's wallet");
    const posts = calls.filter((c) => c.method === 'POST' && c.path === '/v1/admin/credit-adjustments');
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body).toEqual({ accountId: ACC, amountPaise: -30000, reason: 'Duplicate cashback', link: 'TZ-1' });
    expect(posts[1]?.headers.get('Idempotency-Key')).toMatch(KEY);
  });

  it('finds a customer with no wallet yet, adjusts by customer id and searches again (D-240)', async () => {
    const CUST = '01JB0CN1A2B3C4D5E6F7G8H9J0';
    const fresh: AdminLedgerAccount = { id: null, kind: 'customer_wallet', ownerType: 'customer', ownerId: CUST, ownerName: 'Meena', balancePaise: 0 };
    let opened = false;
    const noWallet: Handler = (c) => {
      if (c.path === '/v1/admin/ledger/accounts' && c.method === 'GET') {
        return json(200, { items: [opened ? { ...fresh, id: ACC, balancePaise: 20000 } : fresh], serverNow: NOW });
      }
      if (c.path === '/v1/admin/credit-adjustments' && c.method === 'POST') opened = true;
      return undefined;
    };
    const { calls, history } = renderAt('/credits', { roles: ['finance'], handlers: [noWallet, api().handler] });
    fireEvent.change(await screen.findByLabelText('Phone, store code or id'), { target: { value: CUST } });
    fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    const results = await screen.findByRole('table', { name: 'Accounts' });
    fireEvent.click(within(results).getByRole('button', { name: 'Meena' }));
    const panel = await screen.findByRole('complementary', { name: 'Meena' });
    expect(within(panel).getByText('No ledger lines yet. The wallet opens with the first adjustment.')).toBeInTheDocument();
    expect(within(panel).getByText('₹0')).toBeInTheDocument();
    expect(calls.some((c) => c.path.includes('/entries'))).toBe(false);
    expect(history.location.search).not.toContain('account=');

    fireEvent.click(within(panel).getByRole('button', { name: 'Adjust credits' }));
    const dialog = await screen.findByRole('dialog', { name: 'Adjust credits for Meena' });
    fireEvent.change(within(dialog).getByLabelText('Credits'), { target: { value: '200' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Goodwill' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adjust' }));
    await toastSays("₹200 posted to Meena's wallet");
    const post = calls.find((c) => c.method === 'POST' && c.path === '/v1/admin/credit-adjustments');
    expect(post?.body).toEqual({ customerId: CUST, amountPaise: 20000, reason: 'Goodwill' });
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/ledger/accounts')).toHaveLength(2));
    expect(await within(await screen.findByRole('table', { name: 'Accounts' })).findByText('₹200')).toBeInTheDocument();
  });

  it('says when an adjustment above the threshold waits for a second person', async () => {
    renderAt(`/credits?account=${ACC}`, { roles: ['super_admin'], handlers: [api({ outcome: 'pending' }).handler] });
    const panel = await screen.findByRole('complementary', { name: 'Priya' });
    fireEvent.click(within(panel).getByRole('button', { name: 'Adjust credits' }));
    const dialog = await screen.findByRole('dialog', { name: 'Adjust credits for Priya' });
    fireEvent.change(within(dialog).getByLabelText('Credits'), { target: { value: '800' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Goodwill' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adjust' }));
    await toastSays('₹800 for Priya is waiting for a second person');
  });

  it("disables approve on the caller's own request and lets another person approve with a note", async () => {
    const { calls } = renderAt('/credits?tab=approvals', { roles: ['finance'], handlers: [api().handler] });
    const table = await screen.findByRole('table', { name: 'Adjustment requests' });
    await within(table).findByText('Kiran');
    const rows = within(table).getAllByRole('row');
    const mine = rows.find((r) => r.textContent?.includes('Kiran'));
    const theirs = rows.find((r) => r.textContent?.includes('Priya'));
    if (!mine || !theirs) throw new Error('rows missing');
    expect(within(mine).getByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(within(mine).getByText('Needs a second person')).toBeInTheDocument();
    expect(within(mine).getByRole('button', { name: 'Withdraw' })).toBeEnabled();
    expect(within(theirs).getByText('Goodwill after a failed delivery (TZ-024817)')).toBeInTheDocument();

    fireEvent.click(within(theirs).getByRole('button', { name: 'Approve' }));
    const dialog = await screen.findByRole('dialog', { name: 'Approve ₹800 for Priya?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    expect(await within(dialog).findByText('Write a reason, up to 500 characters.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Checked the order' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    await toastSays('₹800 for Priya approved and posted');
    const approve = calls.find((c) => c.path === `/v1/admin/credit-adjustments/${REQ}/approve`);
    expect(approve?.body).toEqual({ note: 'Checked the order' });
    expect(approve?.headers.get('Idempotency-Key')).toMatch(KEY);
  });

  it('withdraws your own request', async () => {
    const { calls } = renderAt('/credits?tab=approvals', { roles: ['super_admin'], handlers: [api().handler] });
    const table = await screen.findByRole('table', { name: 'Adjustment requests' });
    await within(table).findByText('Kiran');
    const mine = within(table).getAllByRole('row').find((r) => r.textContent?.includes('Kiran'));
    if (!mine) throw new Error('row missing');
    fireEvent.click(within(mine).getByRole('button', { name: 'Withdraw' }));
    const dialog = await screen.findByRole('dialog', { name: 'Withdraw your request of -₹600 for Kiran?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Wrong customer' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Withdraw' }));
    await toastSays('Your request of -₹600 for Kiran withdrawn');
    expect(calls.find((c) => c.path === `/v1/admin/credit-adjustments/${MINE}/reject`)?.body).toEqual({ note: 'Wrong customer' });
  });

  it('keeps the status filter in the URL', async () => {
    const { calls, history } = renderAt('/credits?tab=approvals&status=all', { roles: ['finance'], handlers: [api().handler] });
    await screen.findByRole('table', { name: 'Adjustment requests' });
    const list = calls.filter((c) => c.path === '/v1/admin/credit-adjustments' && c.method === 'GET');
    expect(new URLSearchParams(list[0]?.search).has('status')).toBe(false);
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'approved' } });
    await waitFor(() => expect(history.location.search).toContain('status=approved'));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/credit-adjustments' && c.search.includes('status=approved'))).toBe(true));
  });
});
