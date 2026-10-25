import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages, formatMoney } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const B = '/v1/biz/businesses/b1';
const NOW = '2026-11-02T06:00:00.000Z';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

/** The rest of the credits page answers empty, so only the statements card has content. */
const page: Handler = (c) => {
  if (c.path === `${B}/credit-account`) return json(200, { balance: 0, items: [], nextCursor: null });
  if (c.path === `${B}/referrals`) return json(200, { code: 'SHOP-K7Q2X9', link: 'https://app.todayz.in/join?ref=SHOP-K7Q2X9', rewards: { referrerCredits: 100, refereeCredits: 100 }, stats: { clicks: 0, pending: 0, rewarded: 0, creditsEarned: 0 }, referredBy: null });
  if (c.path === `${B}/stores/s1`) return json(200, { id: 's1', shortCode: 'K7Q2X9' });
  return undefined;
};

const totals = (closingPaise: number, over: Record<string, number> = {}) => ({
  openingPaise: 0,
  creditsInPaise: 0,
  referralInPaise: 0,
  cashbackOutPaise: 0,
  reversalsPaise: 0,
  adjustmentsPaise: 0,
  payoutsOutPaise: 0,
  closingPaise,
  ...over,
});

const october = {
  period: '2026-10',
  closedAt: '2026-10-31T19:00:00Z',
  totals: totals(340000, { openingPaise: 10000, creditsInPaise: 400000, referralInPaise: 10000, cashbackOutPaise: 75000, reversalsPaise: -5000 }),
  payout: { status: 'paid', amountPaise: 340000, utr: 'AXIS1234567890', paidAt: '2026-11-01T09:30:00Z' },
};
const september = { period: '2026-09', closedAt: '2026-09-30T19:00:00Z', totals: totals(120000), payout: { status: 'processing', amountPaise: 120000, utr: null, paidAt: null } };
const august = { period: '2026-08', closedAt: '2026-08-31T19:00:00Z', totals: totals(90000), payout: { status: 'needs_kyc', amountPaise: 90000, utr: null, paidAt: null } };
const july = { period: '2026-07', closedAt: '2026-07-31T19:00:00Z', totals: totals(-30000, { cashbackOutPaise: 30000 }) };

function list(items: unknown[], nextCursor: string | null = null): Handler {
  return (c) => (c.method === 'GET' && c.path === `${B}/statements` ? json(200, { items, nextCursor, serverNow: NOW }) : undefined);
}

afterEach(() => vi.restoreAllMocks());

describe('B-10 monthly statements', () => {
  it('shows each month with its closing, the payout state and the UTR once paid', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), page, list([october, september, august, july])] });

    const oct = await screen.findByTestId('statement-2026-10');
    expect(oct).toHaveTextContent('October 2026');
    expect(oct).toHaveTextContent(`Closing ${formatMoney(340000)}`);
    expect(within(oct).getByText('Paid')).toBeInTheDocument();
    expect(oct).toHaveTextContent(`${formatMoney(340000)} paid on 1 Nov 2026.`);
    expect(oct).toHaveTextContent('UTR AXIS1234567890');

    const sep = screen.getByTestId('statement-2026-09');
    expect(within(sep).getByText('Processing')).toBeInTheDocument();
    expect(sep).toHaveTextContent(`Payout of ${formatMoney(120000)} is being processed.`);
    expect(sep).not.toHaveTextContent('UTR');

    const aug = screen.getByTestId('statement-2026-08');
    expect(within(aug).getByText('On hold')).toBeInTheDocument();
    expect(aug).toHaveTextContent('on hold until your payout details are complete');

    // A negative month makes no payout and carries forward.
    const jul = screen.getByTestId('statement-2026-07');
    expect(jul).toHaveTextContent(`Closing ${formatMoney(-30000)}`);
    expect(jul).toHaveTextContent('No payout this month.');
    expect(jul).toHaveTextContent('A negative balance carries forward to next month.');
  });

  it('breaks a month down, showing cashback and payouts as taken away', async () => {
    renderAt('/credits', { messages, handlers: [shops('manager'), page, list([october])] });
    const oct = await screen.findByTestId('statement-2026-10');
    fireEvent.click(within(oct).getByText('Breakdown'));
    const row = (label: string) => within(oct).getByText(label).nextElementSibling?.textContent;
    expect(row('Opening balance')).toBe(formatMoney(10000));
    expect(row('Credits spent at your shop')).toBe(formatMoney(400000));
    expect(row('Referral rewards')).toBe(formatMoney(10000));
    expect(row('Cashback you gave')).toBe(formatMoney(-75000));
    expect(row('Reversed sales')).toBe(formatMoney(-5000));
    expect(row('Payouts')).toBe(formatMoney(0));
    expect(row('Closing balance')).toBe(formatMoney(340000));
  });

  it('loads older months with the cursor', async () => {
    const { calls } = renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        page,
        (c) => {
          if (c.path !== `${B}/statements`) return undefined;
          return c.search.includes('cursor=c2')
            ? json(200, { items: [september], nextCursor: null, serverNow: NOW })
            : json(200, { items: [october], nextCursor: 'c2', serverNow: NOW });
        },
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Load older statements' }));
    expect(await screen.findByTestId('statement-2026-09')).toBeInTheDocument();
    expect(screen.getByTestId('statement-2026-10')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Load older statements' })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.path === `${B}/statements`).map((c) => c.search)).toEqual(['limit=12', 'limit=12&cursor=c2']);
  });

  it('downloads the CSV with the session token, not a token in the URL', async () => {
    const createObjectURL = vi.fn((_: Blob) => 'blob:statement');
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true });
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    const csv = 'period,store,opening\n2026-10,Indiranagar,100.00\n';
    const { calls } = renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        page,
        list([october]),
        (c) => (c.path === `${B}/statements/2026-10` ? new Response(csv, { status: 200, headers: { 'content-type': 'text/csv; charset=utf-8' } }) : undefined),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Download the October 2026 statement as CSV' }));
    await waitFor(() => expect(click).toHaveBeenCalledTimes(1));
    const anchor = click.mock.contexts[0] as HTMLAnchorElement;
    expect(anchor.download).toBe('todayz-statement-2026-10.csv');
    expect(anchor.href).toBe('blob:statement');
    expect(await createObjectURL.mock.calls[0]?.[0].text()).toBe(csv);
    const req = calls.find((c) => c.path === `${B}/statements/2026-10`);
    if (!req) throw new Error('no CSV request');
    expect(req.search).toBe('format=csv');
    expect(req.headers.get('Accept')).toContain('text/csv');
    expect(req.headers.get('Authorization')).toBe('Bearer test-token');
  });

  it('says so when the CSV cannot be fetched', async () => {
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(), configurable: true });
    renderAt('/credits', {
      messages,
      handlers: [shops('owner'), page, list([october]), (c) => (c.path === `${B}/statements/2026-10` ? problem(500, 'internal') : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Download the October 2026 statement as CSV' }));
    expect(await screen.findByText('The statement could not be downloaded. Try again.')).toBeInTheDocument();
  });

  it('shows an empty month list plainly, and a failed list with a retry', async () => {
    let fail = true;
    renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        page,
        (c) => (c.path === `${B}/statements` ? (fail ? problem(500, 'internal') : json(200, { items: [], nextCursor: null, serverNow: NOW })) : undefined),
      ],
    });
    expect(await screen.findByText('Statements could not be loaded.')).toBeInTheDocument();
    fail = false;
    const card = screen.getByRole('region', { name: 'Monthly statements' });
    fireEvent.click(within(card).getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText(/No statements yet/)).toBeInTheDocument();
  });

  it('is not asked for by a cashier', async () => {
    const { calls } = renderAt('/credits', { messages, handlers: [shops('cashier'), page, list([october])] });
    expect(await screen.findByText(/owners and managers/i)).toBeInTheDocument();
    expect(calls.some((c) => c.path === `${B}/statements`)).toBe(false);
  });
});
