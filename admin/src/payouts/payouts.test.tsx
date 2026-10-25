import type { AdminPayout } from '@todayz/api-client/gen/model/adminPayout';
import type { AdminPayoutDetail } from '@todayz/api-client/gen/model/adminPayoutDetail';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { NOW } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { lastClosedPeriod, validatePayoutsSearch } from './search';

const BID = '01JB0BZ1A2B3C4D5E6F7G8H9J0';
const PID = '01JB0P01A2B3C4D5E6F7G8H9J0';
const SID = '01JB0SZ1A2B3C4D5E6F7G8H9J0';
const KEY = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const base = `/v1/admin/payouts/${PID}`;

const totals = {
  openingPaise: 0,
  creditsInPaise: 300000,
  referralInPaise: 0,
  cashbackOutPaise: 30000,
  reversalsPaise: 0,
  adjustmentsPaise: 0,
  payoutsOutPaise: 0,
  closingPaise: 270000,
};

function payout(over: Partial<AdminPayout> = {}): AdminPayout {
  return {
    id: PID,
    businessId: BID,
    businessName: 'Sri Lakshmi Stores',
    amountPaise: 267000,
    status: 'pending',
    holdReason: null,
    holdNote: null,
    statementId: SID,
    period: '2026-09',
    exportedAt: null,
    computedAt: '2026-09-30T19:00:00Z',
    utr: null,
    paidAt: null,
    paidBy: null,
    createdAt: '2026-09-30T19:00:00Z',
    version: 1,
    ...over,
  };
}

function detail(p: AdminPayout): AdminPayoutDetail {
  return {
    payout: p,
    lines: [{ storeId: '01JB0ST1A2B3C4D5E6F7G8H9J0', storeName: 'Lakshmi Indiranagar', amountPaise: p.amountPaise }],
    causes: { businessSuspended: false, kycVerified: true, payoutDetailsMissing: false, openRiskFlag: false },
    payee: { kycStatus: 'verified', upiIdLast4: '@okx', bankAccountLast4: '4321', ifsc: 'HDFC0001234' },
    serverNow: NOW,
  };
}

interface Api {
  handler: Handler;
  current: AdminPayout;
  /** Money actions answer step_up_required until a step-up verify succeeds. */
  stepUpNeeded: boolean;
}

function api(opts: { stepUp?: boolean; amountNow?: number } = {}): Api {
  const state: Api = { current: payout(), stepUpNeeded: opts.stepUp ?? false, handler: () => undefined };
  state.handler = (c) => {
    if (c.path === '/v1/admin/statements' && c.method === 'GET') {
      return json(200, {
        period: '2026-09',
        items: [
          {
            id: SID,
            businessId: BID,
            businessName: 'Sri Lakshmi Stores',
            businessStatus: 'active',
            period: '2026-09',
            totals,
            kycStatus: 'verified',
            openRiskFlag: false,
            closedAt: '2026-09-30T19:00:00Z',
            payout: { id: PID, status: state.current.status, amountPaise: state.current.amountPaise, holdReason: null, utr: state.current.utr, carried: false },
          },
        ],
        nextCursor: null,
        serverNow: NOW,
      });
    }
    if (c.path === '/v1/admin/payouts' && c.method === 'GET') return json(200, { items: [state.current], nextCursor: null, serverNow: NOW });
    if (c.path === base && c.method === 'GET') return json(200, detail(state.current));
    if (c.path.startsWith(`${base}/`) && c.method === 'POST') {
      if (state.stepUpNeeded) return problem(403, 'step_up_required');
      const body = c.body as { reason: string; utr?: string; amountPaise?: number };
      if (c.path.endsWith('/hold')) state.current = payout({ status: 'held', holdReason: 'manual', holdNote: body.reason });
      if (c.path.endsWith('/unhold')) state.current = payout();
      if (c.path.endsWith('/mark-paid')) {
        if (opts.amountNow !== undefined && body.amountPaise !== opts.amountNow) {
          state.current = payout({ amountPaise: opts.amountNow });
          return problem(409, 'payout_amount_changed');
        }
        state.current = payout({ status: 'paid', utr: body.utr ?? null, paidAt: NOW, amountPaise: body.amountPaise ?? 0 });
      }
      return json(200, detail(state.current));
    }
    if (c.path === '/v1/admin/payout-exports' && c.method === 'GET') {
      if (state.stepUpNeeded) return problem(403, 'step_up_required');
      return new Response('payout_id,period\n', { status: 200, headers: { 'content-type': 'text/csv' } });
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

const at = `/payouts?period=2026-09&payout=${PID}`;

describe('payouts search', () => {
  it('keeps only a real month, status and payout id', () => {
    expect(validatePayoutsSearch({ period: '2026-09', status: 'held', payout: PID.toLowerCase(), x: 1 })).toEqual({ period: '2026-09', status: 'held', payout: PID });
    expect(validatePayoutsSearch({ period: '2026-13', status: 'lost', payout: 'nope' })).toEqual({});
  });

  it('starts on the last IST month that has ended', () => {
    expect(lastClosedPeriod(new Date('2026-10-25T06:30:00Z'))).toBe('2026-09');
    // 1 November 00:10 IST is still 31 October in UTC.
    expect(lastClosedPeriod(new Date('2026-10-31T18:40:00Z'))).toBe('2026-10');
    expect(lastClosedPeriod(new Date('2026-01-05T00:00:00Z'))).toBe('2025-12');
  });
});

describe('A-11 payouts', () => {
  it('shows the month, the payout lines, live causes and masked payee', async () => {
    const { calls } = renderAt(at, { roles: ['finance'], handlers: [api().handler] });
    const statements = await screen.findByRole('table', { name: 'Statements' });
    expect(await within(statements).findByText('₹2,700')).toBeInTheDocument();
    expect(within(statements).getByText('Pending · ₹2,670')).toBeInTheDocument();
    const panel = await screen.findByRole('complementary', { name: 'Sri Lakshmi Stores' });
    expect(within(panel).getByRole('table', { name: 'Per store' })).toHaveTextContent('Lakshmi Indiranagar');
    expect(within(panel).getByText('•••• 4321')).toBeInTheDocument();
    expect(within(panel).getByText('KYC not verified')).toBeInTheDocument();
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/statements')?.search).get('period')).toBe('2026-09');
    expect(within(panel).getByRole('button', { name: 'Mark paid' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeInTheDocument();
  });

  it('opens on the last closed month by the server clock, not the device clock', async () => {
    vi.useFakeTimers({ toFake: ['Date'], now: new Date('2027-03-15T06:30:00Z') });
    try {
      const { calls } = renderAt('/payouts', { roles: ['finance'], handlers: [api().handler] });
      expect(await screen.findByRole('heading', { name: 'Statements for 2026-09' })).toBeInTheDocument();
      await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/statements')).toBe(true));
      expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/statements')?.search).get('period')).toBe('2026-09');
      expect(screen.getByLabelText('Month')).toHaveValue('2026-09');
    } finally {
      vi.useRealTimers();
    }
  });

  it('lets the admin role read without any money action', async () => {
    renderAt(at, { roles: ['admin'], handlers: [api().handler] });
    const panel = await screen.findByRole('complementary', { name: 'Sri Lakshmi Stores' });
    expect(within(panel).getByText('•••• 4321')).toBeInTheDocument();
    expect(within(panel).queryByRole('button', { name: 'Mark paid' })).not.toBeInTheDocument();
    expect(within(panel).queryByRole('button', { name: 'Hold' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Export CSV' })).not.toBeInTheDocument();
  });

  it('marks paid after a step-up with the UTR, the amount on screen, a reason and an Idempotency-Key', async () => {
    const a = api({ stepUp: true });
    const { calls } = renderAt(at, { roles: ['finance'], handlers: [a.handler, stepUpVerify(a)] });
    const panel = await screen.findByRole('complementary', { name: 'Sri Lakshmi Stores' });
    fireEvent.click(within(panel).getByRole('button', { name: 'Mark paid' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mark the payout for Sri Lakshmi Stores paid?' });
    const confirm = () => fireEvent.click(within(dialog).getByRole('button', { name: 'Mark ₹2,670 paid' }));
    fireEvent.change(within(dialog).getByLabelText('UTR'), { target: { value: 'ax 1' } });
    confirm();
    expect(await within(dialog).findByText('Enter the UTR: 12 to 22 letters and digits.')).toBeInTheDocument();
    expect(within(dialog).getByText('Write a reason, up to 500 characters.')).toBeInTheDocument();
    expect(calls.some((c) => c.path === `${base}/mark-paid`)).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('UTR'), { target: { value: 'axis12345678' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'September payout, HDFC batch' } });
    confirm();

    await screen.findByRole('dialog', { name: 'Confirm it is you' });
    await typeCode('123456');
    await toastSays('Payout for Sri Lakshmi Stores marked paid');
    const posts = calls.filter((c) => c.path === `${base}/mark-paid`);
    expect(posts).toHaveLength(2);
    expect(posts[1]?.body).toEqual({ utr: 'AXIS12345678', amountPaise: 267000, reason: 'September payout, HDFC batch' });
    expect(posts[1]?.headers.get('Idempotency-Key')).toMatch(KEY);
    expect(await within(screen.getByRole('complementary', { name: 'Sri Lakshmi Stores' })).findByText(/AXIS12345678/)).toBeInTheDocument();
  });

  it('refuses a recomputed amount and shows the new one', async () => {
    const a = api({ amountNow: 250000 });
    renderAt(at, { roles: ['super_admin'], handlers: [a.handler] });
    const panel = await screen.findByRole('complementary', { name: 'Sri Lakshmi Stores' });
    fireEvent.click(within(panel).getByRole('button', { name: 'Mark paid' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mark the payout for Sri Lakshmi Stores paid?' });
    fireEvent.change(within(dialog).getByLabelText('UTR'), { target: { value: 'AXIS12345678' } });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'September payout' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mark ₹2,670 paid' }));
    expect(await within(dialog).findByText('The amount changed since you opened this payout. Check the new amount and confirm again.')).toBeInTheDocument();
    expect(await within(dialog).findByRole('button', { name: 'Mark ₹2,500 paid' })).toBeInTheDocument();
  });

  it('holds with a reason, then releases', async () => {
    const { calls } = renderAt(at, { roles: ['finance'], handlers: [api().handler] });
    const panel = await screen.findByRole('complementary', { name: 'Sri Lakshmi Stores' });
    fireEvent.click(within(panel).getByRole('button', { name: 'Hold' }));
    const dialog = await screen.findByRole('dialog', { name: 'Hold the payout for Sri Lakshmi Stores?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Bank details under review' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hold' }));
    await toastSays('Payout for Sri Lakshmi Stores held');
    expect(calls.find((c) => c.path === `${base}/hold`)?.body).toEqual({ reason: 'Bank details under review' });
    expect(await within(panel).findByText('Held by hand: Bank details under review')).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole('button', { name: 'Release hold' }));
    const release = await screen.findByRole('dialog', { name: 'Release the hold for Sri Lakshmi Stores?' });
    fireEvent.change(within(release).getByLabelText('Reason'), { target: { value: 'Bank details confirmed' } });
    fireEvent.click(within(release).getByRole('button', { name: 'Release hold' }));
    await toastSays('Payout for Sri Lakshmi Stores released');
    await waitFor(() => expect(within(panel).getByRole('button', { name: 'Mark paid' })).toBeInTheDocument());
  });

  it('exports the month after a step-up, with the CSV Accept header', async () => {
    const a = api({ stepUp: true });
    const { calls } = renderAt('/payouts?period=2026-09', { roles: ['finance'], handlers: [a.handler, stepUpVerify(a)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Export CSV' }));
    await screen.findByRole('dialog', { name: 'Confirm it is you' });
    await typeCode('123456');
    await toastSays('Payout file for 2026-09 saved. It holds bank details: keep it private.');
    const gets = calls.filter((c) => c.path === '/v1/admin/payout-exports');
    expect(gets).toHaveLength(2);
    expect(new URLSearchParams(gets[1]?.search).get('period')).toBe('2026-09');
    expect(gets[1]?.headers.get('Accept')).toBe('text/csv');
  });
});
