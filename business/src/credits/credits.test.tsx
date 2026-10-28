import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { failThenHold, holding } from '../test/held';

const messages = asMessages(en);
const B = '/v1/biz/businesses/b1';
const CODE = 'SHOP-K7Q2X9';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

const line = (id: string, kind: string, credits: number, ref: { kind: string; label: string } | null = null) => ({
  id,
  kind,
  credits,
  at: '2026-10-20T08:00:00.000Z',
  ref,
});

const referrals = { code: CODE, link: `https://app.todayz.in/join?ref=${CODE}`, rewards: { referrerCredits: 100, refereeCredits: 100 }, stats: { clicks: 9, pending: 2, rewarded: 1, creditsEarned: 100 }, referredBy: null };

function account(items: unknown[], nextCursor: string | null = null, balance = 340): Handler {
  return (c) => (c.method === 'GET' && c.path === `${B}/credit-account` ? json(200, { balance, items, nextCursor }) : undefined);
}
const refs: Handler = (c) => (c.method === 'GET' && c.path === `${B}/referrals` ? json(200, referrals) : undefined);
const store: Handler = (c) => (c.method === 'GET' && c.path === `${B}/stores/s1` ? json(200, { id: 's1', shortCode: 'K7Q2X9' }) : undefined);

let writeText: ReturnType<typeof vi.fn>;
beforeEach(() => {
  writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});
afterEach(() => vi.restoreAllMocks());

describe('B-10 credit account', () => {
  it('shows the balance and a sign per line kind, with an order reference and no customer detail', async () => {
    renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        account([
          line('l1', 'spent', 30, { kind: 'order', label: 'TZ-024817' }),
          line('l2', 'cashback', 12),
          line('l3', 'referral_reward', 100),
          line('l4', 'pos_reversal', 30, { kind: 'sale', label: 'S-118' }),
        ]),
        refs,
        store,
      ],
    });
    expect(await screen.findByTestId('credit-balance')).toHaveTextContent('340 credits');
    const list = screen.getByRole('list', { name: 'Credit account lines' });
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(4);
    expect(items[0]).toHaveTextContent('Credits spent at your shop');
    expect(items[0]).toHaveTextContent('Order TZ-024817');
    expect(items[0]).toHaveTextContent('+30');
    expect(items[1]).toHaveTextContent('Cashback you gave');
    expect(items[1]).toHaveTextContent('−12');
    expect(items[2]).toHaveTextContent('+100');
    expect(items[3]).toHaveTextContent('Sale reversed');
    expect(items[3]).toHaveTextContent('−30');
  });

  it('loads older lines with the cursor', async () => {
    const calls = renderAt('/credits', {
      messages,
      handlers: [
        shops('manager'),
        (c) => {
          if (c.path !== `${B}/credit-account`) return undefined;
          return c.search.includes('cursor=c2')
            ? json(200, { balance: 340, items: [line('l9', 'cashback', 5)], nextCursor: null })
            : json(200, { balance: 340, items: [line('l1', 'spent', 30)], nextCursor: 'c2' });
        },
        refs,
        store,
      ],
    }).calls;
    fireEvent.click(await screen.findByRole('button', { name: 'Show older lines' }));
    await waitFor(() => expect(screen.getAllByRole('listitem').length).toBeGreaterThanOrEqual(2));
    expect(screen.queryByRole('button', { name: 'Show older lines' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.search.includes('cursor=c2'))).toBe(true);
  });

  it('shows an empty state when there are no lines yet', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([], null, 0), refs, store] });
    expect(await screen.findByText('No credit activity yet')).toBeInTheDocument();
  });

  it('shows an error with retry when the account fails, and recovers', async () => {
    let fail = true;
    renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        (c) => (c.path === `${B}/credit-account` ? (fail ? problem(500, 'internal') : json(200, { balance: 7, items: [], nextCursor: null })) : undefined),
        refs,
        store,
      ],
    });
    expect(await screen.findByText('Could not load your credit account.')).toBeInTheDocument();
    fail = false;
    fireEvent.click(screen.getAllByRole('button', { name: 'Try again' }).at(0) as HTMLElement);
    expect(await screen.findByTestId('credit-balance')).toHaveTextContent('7 credits');
  });

  it('explains payouts and shows the monthly statements card (W3-01, src/statements/)', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, store] });
    expect(await screen.findByRole('heading', { name: 'How payouts work' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Monthly statements' })).toBeInTheDocument();
  });
});

describe('B-10 gating and states', () => {
  it('tells a cashier to ask a manager and makes no credit calls', async () => {
    const { calls } = renderAt('/credits', { messages, handlers: [shops('cashier'), account([]), refs, store] });
    expect(await screen.findByText('Ask a manager')).toBeInTheDocument();
    expect(calls.some((c) => c.path.includes('/credit-account') || c.path.includes('/referrals'))).toBe(false);
  });

  it('sends a person with no shop to shop setup', async () => {
    const { history } = renderAt('/credits', {
      messages,
      handlers: [(c) => (c.path === '/v1/biz/businesses' ? json(200, { items: [], invitations: [], nextCursor: null }) : undefined)],
    });
    await waitFor(() => expect(history.location.pathname).toBe('/onboarding'));
  });

  it('shows an error with retry when the shop list fails', async () => {
    renderAt('/credits', { messages, handlers: [(c) => (c.path === '/v1/biz/businesses' ? problem(500, 'internal') : undefined)] });
    expect(await screen.findByText('Could not load your shops')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });
});

describe('B-10 standee and referrals', () => {
  it('shows the store short code and the standee actions, with a plain link to /standee', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, store] });
    expect(await screen.findByText('K7Q2X9')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Preview PDF' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Download A5 PDF' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Standee options' })).toHaveAttribute('href', '/standee');
  });

  it('fetches the standee PDF with the session token and no token in the URL', async () => {
    const createObjectURL = vi.fn(() => 'blob:standee');
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true });
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true });
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    const { calls } = renderAt('/credits', {
      messages,
      handlers: [
        shops('owner'),
        account([]),
        refs,
        store,
        (c) => (c.path === `${B}/stores/s1/qr-standee.pdf` ? new Response(new Blob(['%PDF']), { status: 200, headers: { 'content-type': 'application/pdf' } }) : undefined),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Preview PDF' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith('blob:standee', '_blank', 'noopener'));
    const pdf = calls.find((c) => c.path.endsWith('/qr-standee.pdf'));
    if (!pdf) throw new Error('no standee request');
    expect(pdf.headers.get('Authorization')).toBe('Bearer test-token');
    expect(pdf.search).not.toContain('token');
  });

  it('shows the referral code, stats and where rewards go, and copies the code on tap', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, store] });
    expect(await screen.findByText(CODE)).toBeInTheDocument();
    expect(screen.getByText('2 shops')).toBeInTheDocument();
    expect(screen.getByText('1 shop')).toBeInTheDocument();
    expect(screen.getByText('100 credits')).toBeInTheDocument();
    expect(screen.getByText(/primary store/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(CODE));
    expect(await screen.findByText('Code copied')).toBeInTheDocument();
  });

  it('says so when the clipboard refuses, and keeps the code on screen', async () => {
    writeText.mockRejectedValue(new Error('denied'));
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, store] });
    fireEvent.click(await screen.findByRole('button', { name: 'Copy code' }));
    expect(await screen.findByText(/Could not copy/)).toBeInTheDocument();
    expect(screen.getByText(CODE)).toBeInTheDocument();
  });

  it('copies the share link', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, store] });
    fireEvent.click(await screen.findByRole('button', { name: 'Copy link' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(referrals.link));
  });

  it('never puts the referral code in a request URL', async () => {
    const { calls } = renderAt('/credits', { messages, handlers: [shops('owner'), account([line('l1', 'spent', 30)]), refs, store] });
    fireEvent.click(await screen.findByRole('button', { name: 'Copy code' }));
    await waitFor(() => expect(writeText).toHaveBeenCalled());
    expect(calls.length).toBeGreaterThan(2);
    for (const c of calls) {
      expect(c.path).not.toContain(CODE);
      expect(c.search).not.toContain(CODE);
    }
  });

  it('shows an error with retry when referrals fail', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), (c) => (c.path === `${B}/referrals` ? problem(500, 'internal') : undefined), store] });
    expect(await screen.findByText('Could not load your referral code.')).toBeInTheDocument();
  });
});

describe('B-10 loading states', () => {
  it('shows a skeleton for the short code while the store loads', async () => {
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), refs, holding((c) => c.method === 'GET' && c.path === `${B}/stores/s1`)] });
    await waitFor(() => expect(document.getElementById('cr-standee-title')).not.toBeNull());
    const card = document.getElementById('cr-standee-title')?.closest('section') as HTMLElement;
    expect(card.querySelector('.tz-ui-skeleton')).not.toBeNull();
  });

  it('referral "Try again" shows the loading state while the retry runs', async () => {
    const r = failThenHold((c) => c.path === `${B}/referrals`);
    renderAt('/credits', { messages, handlers: [shops('owner'), account([]), r, store] });
    expect(await screen.findByText('Could not load your referral code.')).toBeInTheDocument();
    const card = document.getElementById('cr-ref-title')?.closest('section') as HTMLElement;
    fireEvent.click(within(card).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(r.calls()).toBe(2));
    await waitFor(() => expect(card.querySelector('.tz-ui-skeleton, [aria-busy="true"]')).not.toBeNull());
  });
});
