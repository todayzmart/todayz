// C-17 Scan bill (`/scan/$token`, the POS bill QR opened from the phone camera) and C-24 the public
// receipt (`/r/$token`, D-45).
import type { BillClaimPreview } from '@todayz/api-client/gen/model/billClaimPreview';
import type { Receipt } from '@todayz/api-client/gen/model/receipt';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const CLAIM = '01JB0V3C1A2J3M4B5K6Q7R8S9T';
const TOKEN = 'EXAMPLEbillQRtokenEXAMPLEbillQRtokenEXAMPLE';
const RECEIPT = 'EXAMPLEreceiptToken_xx';
const NOW = '2026-10-24T05:00:30.000Z';

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on =
  (method: string, path: string, make: (c: Call) => Response): Handler =>
  (c) =>
    c.method === method && c.path === path ? make(c) : undefined;

function preview(over: Partial<BillClaimPreview> = {}): BillClaimPreview {
  return {
    claimId: CLAIM,
    store: { id: '01JB0S3M4N5P6Q7R8S9T0V1W2X', name: 'Sri Lakshmi Stores' },
    amountPaise: 14050,
    maxCredits: 30,
    creditsLimit: null,
    expiresAt: '2026-10-24T05:02:00.000Z',
    serverNow: NOW,
    ...over,
  };
}

function receipt(over: Partial<Receipt> = {}): Receipt {
  return {
    store: { name: 'Sri Lakshmi Stores', addressLine: '12, 4th Cross', locality: 'Jayanagar' },
    lines: [
      { name: 'Amul Butter 100 g', unitPricePaise: 5800, quantity: 2, discountPaise: 0, lineTotalPaise: 11600 },
      { name: 'Sugar 1 kg', unitPricePaise: 6000, quantity: 1, discountPaise: 500, lineTotalPaise: 5500 },
    ],
    itemSubtotalPaise: 17100,
    discountPaise: 0,
    creditsUsed: 20,
    roundOffPaise: 0,
    totalPaise: 15100,
    paymentMethod: 'upi',
    status: 'completed',
    soldAt: '2026-10-24T05:00:00.000Z',
    ...over,
  };
}

describe('C-17 scan bill', () => {
  it('sends a signed-out person to sign in and back here', async () => {
    const { router } = renderAt(`/scan/${TOKEN}`);
    await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
    expect(router.state.location.searchStr).toContain(encodeURIComponent(`/scan/${TOKEN}`));
  });

  it('shows the bill, and links it with the credits chosen', async () => {
    const { calls } = renderAt(`/scan/${TOKEN}`, {
      returning: true,
      handlers: [
        signedIn,
        on('POST', `/v1/me/bill-claims/${TOKEN}/preview`, () => json(200, preview())),
        on('POST', `/v1/me/bill-claims/${TOKEN}/confirm`, (c) => {
          const credits = (c.body as { creditsToUse: number }).creditsToUse;
          return json(200, { claimId: CLAIM, store: preview().store, amountPaise: 14050, creditsUsed: credits, payablePaise: 14050 - credits * 100, status: 'claimed', serverNow: NOW });
        }),
      ],
    });
    const card = await screen.findByRole('region', { name: 'Link this bill' });
    expect(within(card).getByRole('heading', { name: '₹140.50 at Sri Lakshmi Stores' })).toBeInTheDocument();
    expect(within(card).getByText(/up to 30 credits/)).toBeInTheDocument();
    fireEvent.click(within(card).getByRole('switch', { name: 'Use credits' }));
    fireEvent.change(within(card).getByRole('slider', { name: 'Credits to use' }), { target: { value: '25' } });
    fireEvent.click(within(card).getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByRole('heading', { name: 'Bill linked' })).toBeInTheDocument();
    expect(screen.getByText('Pay ₹115.50 at the counter.')).toBeInTheDocument();
    expect(screen.getByText('25 credits used.')).toBeInTheDocument();
    const confirm = calls.find((c) => c.path.endsWith('/confirm'));
    expect(confirm?.body).toEqual({ creditsToUse: 25 });
    expect(confirm?.headers.get('Idempotency-Key')).toMatch(/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/);
  });

  it('uses no credits unless the person turns them on', async () => {
    const { calls } = renderAt(`/scan/${TOKEN}`, {
      returning: true,
      handlers: [
        signedIn,
        on('POST', `/v1/me/bill-claims/${TOKEN}/preview`, () => json(200, preview())),
        on('POST', `/v1/me/bill-claims/${TOKEN}/confirm`, () =>
          json(200, { claimId: CLAIM, store: preview().store, amountPaise: 14050, creditsUsed: 0, payablePaise: 14050, status: 'claimed', serverNow: NOW }),
        ),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('Pay ₹140.50 at the counter.')).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith('/confirm'))?.body).toEqual({ creditsToUse: 0 });
  });

  it("says why credits can't be used", async () => {
    renderAt(`/scan/${TOKEN}`, {
      returning: true,
      handlers: [signedIn, on('POST', `/v1/me/bill-claims/${TOKEN}/preview`, () => json(200, preview({ maxCredits: 0, creditsLimit: 'store_credit_cap' })))],
    });
    expect(await screen.findByText("Credits can't be used at this shop today.")).toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Use credits' })).toBeDisabled();
  });

  it('says when the QR has expired or was used by someone else', async () => {
    const { unmount } = renderAt(`/scan/${TOKEN}`, {
      returning: true,
      handlers: [signedIn, on('POST', `/v1/me/bill-claims/${TOKEN}/preview`, () => problem(409, 'claim_expired'))],
    });
    expect(await screen.findByText('This QR has expired')).toBeInTheDocument();
    unmount();

    renderAt(`/scan/${TOKEN}`, {
      returning: true,
      handlers: [
        signedIn,
        on('POST', `/v1/me/bill-claims/${TOKEN}/preview`, () => json(200, preview())),
        on('POST', `/v1/me/bill-claims/${TOKEN}/confirm`, () => problem(409, 'claim_already_used')),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('This bill is already linked')).toBeInTheDocument();
  });
});

describe('C-24 receipt', () => {
  it('shows the shop, the items and the totals without signing in, and is not indexed', async () => {
    renderAt(`/r/${RECEIPT}`, { handlers: [on('GET', `/v1/public/receipts/${RECEIPT}`, () => json(200, receipt()))] });
    const card = await screen.findByRole('region', { name: 'Sri Lakshmi Stores' });
    expect(within(card).getByText('12, 4th Cross, Jayanagar')).toBeInTheDocument();
    const items = within(card).getByRole('list', { name: 'Items' });
    expect(within(items).getByText('2 × ₹58.00')).toBeInTheDocument();
    expect(within(items).getByText(/₹5.00 off/)).toBeInTheDocument();
    expect(within(card).getByText('−₹20.00')).toBeInTheDocument();
    expect(within(card).getByText('₹151.00')).toBeInTheDocument();
    expect(within(card).getByText('Paid by UPI')).toBeInTheDocument();
    expect(document.head.querySelector('meta[name="robots"][content="noindex"]')).not.toBeNull();
  });

  it('marks a reversed sale', async () => {
    renderAt(`/r/${RECEIPT}`, { handlers: [on('GET', `/v1/public/receipts/${RECEIPT}`, () => json(200, receipt({ status: 'reversed' })))] });
    expect(await screen.findByText('This sale was reversed by the shop.')).toBeInTheDocument();
  });

  it('says an unknown or old link has expired', async () => {
    renderAt(`/r/${RECEIPT}`);
    expect(await screen.findByText('This receipt link has expired')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Go to home' })).toBeInTheDocument();
  });
});
