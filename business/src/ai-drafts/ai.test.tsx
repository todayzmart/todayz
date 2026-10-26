import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import type { ShelfPhotoLine } from '@todayz/api-client/gen/model/shelfPhotoLine';
import type { SupplierBillLine } from '@todayz/api-client/gen/model/supplierBillLine';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { decideShelf, startShelf } from '../onboarding/shelf/lines';
import { decide, startEdit } from '../restock-scan/lines';
import { INSIGHTS, ownerApi, shops, STORE } from '../test/insightsApi';
import { json, renderAt, type Call, type Handler } from '../test/render';
import { holding } from '../test/held';
import { bannerDecisions } from './bannerDecisions';
import { checkPhoto } from './photo';
import { replyDecision } from './replyDraft';

const messages = asMessages(en);
const AI = `${STORE}/ai`;
const MB = 1024 * 1024;

const ON = { supplierBill: true, shelfPhoto: true, reviewReply: true, bannerCopy: true, maxPhotoBytes: 8 * MB, photoTypes: ['image/jpeg', 'image/png'] };

function features(over: Partial<typeof ON> = {}): Handler {
  return (c) => (c.method === 'GET' && c.path === `${AI}/retail` ? json(200, { ...ON, ...over }) : undefined);
}

const base = { promptVersion: 'v1', createdAt: '2026-10-24T08:00:00.000Z', outcomes: [], failure: null };

const billLines: SupplierBillLine[] = [
  { lineId: 'l1', name: 'AMUL BTR 100G', quantity: 6, costPaise: 5400, mrpPaise: 6000, expiresOn: null, confidence: 0.94, match: { storeItemId: 'si1', itemName: 'Amul Butter 100 g', onHand: 3 } },
  { lineId: 'l2', name: 'TATA SALT 1KG', quantity: 12, costPaise: 2400, mrpPaise: null, expiresOn: null, confidence: 0.81, match: { storeItemId: 'si2', itemName: 'Tata Salt 1 kg', onHand: 20 } },
  { lineId: 'l3', name: 'QZXV WJK', quantity: 2, costPaise: null, mrpPaise: null, expiresOn: null, confidence: 0.4, match: null },
];

function billDraft(status = 'ready') {
  return { ...base, draftId: 'd1', feature: 'supplier-bill', status, supplier: 'Sri Venkateshwara Agencies', billDate: null, lines: status === 'ready' ? billLines : [] };
}

/** The bill scan: create, read (processing first, then `status`), confirm. Every call lands in `calls`. */
function bill(calls: Call[], opts: { status?: string; confirm?: (c: Call) => Response } = {}): Handler {
  let reads = 0;
  return (c) => {
    if (!c.path.startsWith(`${AI}/supplier-bill/drafts`)) return undefined;
    calls.push(c);
    if (c.method === 'POST' && c.path.endsWith('/drafts')) return json(202, { draftId: 'd1' });
    if (c.method === 'GET') return json(200, billDraft(reads++ === 0 ? 'processing' : (opts.status ?? 'ready')));
    if (c.method === 'POST' && c.path.endsWith('/d1/confirm')) {
      if (opts.confirm) return opts.confirm(c);
      const lines = (c.body as { lines: { lineId: string; outcome: string }[] }).lines;
      const all = lines.every((l) => l.outcome === 'discarded');
      return json(200, { draftId: 'd1', status: all ? 'discarded' : 'confirmed', lines: lines.map((l) => ({ lineId: l.lineId, outcome: l.outcome })) });
    }
    return undefined;
  };
}

/** A record entry the test knows is there. */
const at = <T,>(r: Record<string, T>, k: string) => r[k] as T;

const photo = (type = 'image/jpeg', size = 1000) => new File([new Uint8Array(size)], 'bill.jpg', { type });

async function pick(file: File) {
  fireEvent.change(await screen.findByLabelText('Take or pick a photo'), { target: { files: [file] } });
}

describe('B-17 supplier bill scan', () => {
  it('reads the bill as a draft, writes nothing until confirm, then sends one keyed confirm (SC-AI-11, SC-AI-13)', async () => {
    const calls: Call[] = [];
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), bill(calls)] });
    await pick(photo());
    expect(await screen.findByText('Reading the bill…', {}, { timeout: 4000 })).toBeInTheDocument();
    const list = await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    expect(screen.getByText('From Sri Venkateshwara Agencies')).toBeInTheDocument();
    expect(screen.getByText(/Suggested by AI/)).toBeInTheDocument();
    expect(within(list).getByRole('checkbox', { name: /QZXV WJK/ })).toBeDisabled();
    expect(screen.getByText(/Add the item on the “Stock” page first/)).toBeInTheDocument();
    expect(calls.filter((c) => c.path.endsWith('/confirm'))).toHaveLength(0);

    // The salt line gets a new quantity; the butter line stays as read.
    const salt = within(list).getByRole('listitem', { name: 'Tata Salt 1 kg' });
    fireEvent.change(within(salt).getByLabelText('Quantity'), { target: { value: '10' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 2 items' }));
    expect(await screen.findByText('Stock added for 2 items')).toBeInTheDocument();

    const confirms = calls.filter((c) => c.path.endsWith('/confirm'));
    expect(confirms).toHaveLength(1);
    expect(calls.find((c) => c.method === 'POST' && c.path.endsWith('/drafts'))?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(confirms[0]?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(confirms[0]?.body).toEqual({
      lines: [
        { lineId: 'l1', outcome: 'accepted' },
        { lineId: 'l2', outcome: 'edited', value: { storeItemId: 'si2', quantity: 10, costPaise: 2400, expiresOn: null } },
        { lineId: 'l3', outcome: 'discarded' },
      ],
    });
  });

  it('stops on a bad quantity before sending, and shows the server line reason (SC-AI-12)', async () => {
    const calls: Call[] = [];
    renderAt('/restock-scan', {
      messages,
      handlers: [
        shops('owner'),
        features(),
        bill(calls, { confirm: () => json(422, { type: 'about:blank#validation_failed', title: 'x', status: 422, code: 'validation_failed', fields: { 'body.lines[0].value.costPaise': 'above_mrp' } }) }),
      ],
    });
    await pick(photo());
    const list = await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    const butter = within(list).getByRole('listitem', { name: 'Amul Butter 100 g' });
    fireEvent.change(within(butter).getByLabelText('Quantity'), { target: { value: '0' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 2 items' }));
    expect(await within(butter).findByText('Enter a whole number')).toBeInTheDocument();
    expect(calls.filter((c) => c.path.endsWith('/confirm'))).toHaveLength(0);

    fireEvent.change(within(butter).getByLabelText('Quantity'), { target: { value: '6' } });
    fireEvent.change(within(butter).getByLabelText('Cost price'), { target: { value: '70' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 2 items' }));
    expect(await within(butter).findByText('More than the MRP')).toBeInTheDocument();
  });

  it('applies the good lines, keeps the refused one open with its reason, and confirms it again (SC-AI-12)', async () => {
    const calls: Call[] = [];
    let n = 0;
    const confirm = (c: Call) => {
      const lines = (c.body as { lines: { lineId: string; outcome: string }[] }).lines;
      // The first confirm refuses the butter line (cost above the MRP); the second takes it.
      if (n++ === 0) return json(200, { draftId: 'd1', status: 'ready', lines: lines.map((l) => (l.lineId === 'l1' ? { lineId: 'l1', outcome: 'not_permitted', reason: 'invalid_cost_above_mrp' } : { lineId: l.lineId, outcome: l.outcome })) });
      return json(200, { draftId: 'd1', status: 'confirmed', lines: [{ lineId: 'l1', outcome: 'edited' }, { lineId: 'l2', outcome: 'accepted' }, { lineId: 'l3', outcome: 'discarded' }] });
    };
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), bill(calls, { confirm })] });
    await pick(photo());
    const list = await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 2 items' }));

    expect(await screen.findByText(/Fix the lines marked below and confirm them again\. 1 line was added\./)).toBeInTheDocument();
    const butter = within(list).getByRole('listitem', { name: 'Amul Butter 100 g' });
    const salt = within(list).getByRole('listitem', { name: 'Tata Salt 1 kg' });
    expect(within(butter).getByRole('alert')).toHaveTextContent('More than the MRP');
    expect(within(salt).getByText('Added')).toBeInTheDocument();
    expect(within(salt).getByRole('checkbox')).toBeDisabled();
    expect(within(salt).queryByLabelText('Quantity')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Discard the rest' })).toBeInTheDocument();

    // The person fixes the cost; only the open line goes again, under a new key.
    fireEvent.change(within(butter).getByLabelText('Cost price'), { target: { value: '50' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 1 item' }));
    expect(await screen.findByText('Stock added for 2 items')).toBeInTheDocument();
    const confirms = calls.filter((c) => c.path.endsWith('/confirm'));
    expect(confirms).toHaveLength(2);
    // The salt and the unmatched line were settled by the first confirm and are not sent again.
    expect(confirms[1]?.body).toEqual({ lines: [{ lineId: 'l1', outcome: 'edited', value: { storeItemId: 'si1', quantity: 6, costPaise: 5000, expiresOn: null } }] });
    expect(confirms[1]?.headers.get('Idempotency-Key')).not.toEqual(confirms[0]?.headers.get('Idempotency-Key'));
  });

  it('says a refused line needs another role', async () => {
    const confirm = (c: Call) => {
      const lines = (c.body as { lines: { lineId: string; outcome: string }[] }).lines;
      return json(200, { draftId: 'd1', status: 'ready', lines: lines.map((l) => (l.outcome === 'discarded' ? { lineId: l.lineId, outcome: 'discarded' } : { lineId: l.lineId, outcome: 'not_permitted', reason: 'not_permitted' })) });
    };
    renderAt('/restock-scan', { messages, handlers: [shops('manager'), features(), bill([], { confirm })] });
    await pick(photo());
    await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    fireEvent.click(screen.getByRole('button', { name: 'Add stock for 2 items' }));
    expect(await screen.findAllByText('Your role cannot add this line. Ask the owner or a manager.')).toHaveLength(2);
    expect(screen.getByText(/Nothing was added yet/)).toBeInTheDocument();
  });

  it('says when other photos are still being read (429)', async () => {
    renderAt('/restock-scan', {
      messages,
      handlers: [shops('owner'), features(), (c) => (c.method === 'POST' && c.path.endsWith('/supplier-bill/drafts') ? json(429, { type: 'about:blank#rate_limited', title: 'x', status: 429, code: 'rate_limited' }, { 'retry-after': '90' }) : undefined)],
    });
    await pick(photo());
    expect(await screen.findByText('Other photos are still being read. Try again in a minute or two.')).toBeInTheDocument();
  });

  it('discards the whole draft with every line discarded', async () => {
    const calls: Call[] = [];
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), bill(calls)] });
    await pick(photo());
    await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    fireEvent.click(screen.getByRole('button', { name: 'Discard draft' }));
    expect(await screen.findByText('Draft discarded. Nothing was added.')).toBeInTheDocument();
    const body = calls.find((c) => c.path.endsWith('/confirm'))?.body as { lines: { outcome: string }[] };
    expect(body.lines.map((l) => l.outcome)).toEqual(['discarded', 'discarded', 'discarded']);
  });

  it('checks the photo in the browser and never uploads a wrong one (SC-AI-23)', async () => {
    const calls: Call[] = [];
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), bill(calls)] });
    expect(await screen.findByText('A clear photo in good light, up to 8 MB')).toBeInTheDocument();
    await pick(photo('application/pdf'));
    expect(await screen.findByText(/not a photo we can read/)).toBeInTheDocument();
    await pick(photo('image/jpeg', 9 * MB));
    expect(await screen.findByText('This photo is over 8 MB. Pick a smaller one.')).toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });

  it('says when the daily AI cap lifts (SC-AI-25)', async () => {
    renderAt('/restock-scan', {
      messages,
      handlers: [shops('owner'), features(), (c) => (c.method === 'POST' && c.path.endsWith('/supplier-bill/drafts') ? json(429, { type: 'about:blank#ai_daily_cap_reached', title: 'x', status: 429, code: 'ai_daily_cap_reached' }, { 'retry-after': '600' }) : undefined)],
    });
    await pick(photo());
    expect(await screen.findByText('You have used the AI help for now. Try again in 10 minutes.')).toBeInTheDocument();
  });

  it('shows a failed read with Try again', async () => {
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), bill([], { status: 'failed' })] });
    await pick(photo());
    expect(await screen.findByText(/We could not read this bill/, {}, { timeout: 4000 })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByLabelText('Take or pick a photo')).toBeInTheDocument();
  });

  it('is an empty state when the feature is off, and no photo can be picked (SC-AI-24)', async () => {
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features({ supplierBill: false })] });
    expect(await screen.findByText('Bill scan is not on for your shop')).toBeInTheDocument();
    expect(screen.queryByLabelText('Take or pick a photo')).not.toBeInTheDocument();
  });
});

const shelfLines: ShelfPhotoLine[] = [
  { lineId: 'l1', name: 'Maggi 2-min Noodles', brand: 'Nestle', size: '70 g', confidence: 0.9, product: { productId: 'p1', name: 'Maggi Masala Noodles 70 g', brand: 'Nestle', size: '70 g', mrpPaise: 1400 }, listed: false, pricePaise: null },
  { lineId: 'l2', name: 'Parle-G', brand: null, size: null, confidence: 0.9, product: { productId: 'p2', name: 'Parle-G 250 g', brand: 'Parle', size: '250 g', mrpPaise: 3000 }, listed: false, pricePaise: null },
  { lineId: 'l3', name: 'Good Day', brand: null, size: null, confidence: 0.8, product: { productId: 'p3', name: 'Good Day 100 g', brand: 'Britannia', size: '100 g', mrpPaise: 2000 }, listed: true, pricePaise: 1900 },
];

function shelf(calls: Call[], confirm?: (c: Call) => Response): Handler {
  return (c) => {
    if (!c.path.startsWith(`${AI}/shelf-photo/drafts`)) return undefined;
    calls.push(c);
    if (c.method === 'POST' && c.path.endsWith('/drafts')) return json(202, { draftId: 'd2' });
    if (c.method === 'GET') return json(200, { ...base, draftId: 'd2', feature: 'shelf-photo', status: 'ready', lines: shelfLines });
    if (confirm) return confirm(c);
    const lines = (c.body as { lines: { lineId: string; outcome: string }[] }).lines;
    return json(200, { draftId: 'd2', status: 'confirmed', lines });
  };
}

describe('B-18 shelf photo', () => {
  it('adds only the ticked products, at the MRP or the edited price (SC-AI-15, SC-AI-16)', async () => {
    const calls: Call[] = [];
    renderAt('/onboarding/shelf', { messages, handlers: [shops('owner'), features(), shelf(calls)] });
    await pick(photo());
    const list = await screen.findByRole('list', { name: 'Products found' }, { timeout: 4000 });
    expect(within(list).getByText('Already in your stock')).toBeInTheDocument();
    const parle = within(list).getByRole('listitem', { name: 'Parle-G 250 g' });
    fireEvent.change(within(parle).getByLabelText('Your price'), { target: { value: '28' } });
    fireEvent.change(within(parle).getByLabelText('Opening stock'), { target: { value: '5' } });
    expect(calls.filter((c) => c.path.endsWith('/confirm'))).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: 'Add 2 products' }));
    expect(await screen.findByText('2 products added to your stock')).toBeInTheDocument();
    const confirm = calls.find((c) => c.path.endsWith('/confirm'));
    expect(confirm?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(confirm?.body).toEqual({
      lines: [
        { lineId: 'l1', outcome: 'accepted' },
        { lineId: 'l2', outcome: 'edited', value: { productId: 'p2', pricePaise: 2800, openingStock: 5 } },
        { lineId: 'l3', outcome: 'discarded' },
      ],
    });
  });

  it('adds the good products and keeps one listed meanwhile open with its reason (SC-AI-16)', async () => {
    const calls: Call[] = [];
    const confirm = (c: Call) => {
      const lines = (c.body as { lines: { lineId: string; outcome: string }[] }).lines;
      return json(200, { draftId: 'd2', status: 'ready', lines: lines.map((l) => (l.lineId === 'l2' ? { lineId: 'l2', outcome: 'not_permitted', reason: 'invalid_listed' } : { lineId: l.lineId, outcome: l.outcome })) });
    };
    renderAt('/onboarding/shelf', { messages, handlers: [shops('owner'), features(), shelf(calls, confirm)] });
    await pick(photo());
    const list = await screen.findByRole('list', { name: 'Products found' }, { timeout: 4000 });
    fireEvent.click(screen.getByRole('button', { name: 'Add 2 products' }));
    expect(await screen.findByText(/1 line was added/)).toBeInTheDocument();
    const parle = within(list).getByRole('listitem', { name: 'Parle-G 250 g' });
    expect(within(parle).getByRole('alert')).toHaveTextContent('Already in your stock');
    expect(within(list).getByRole('listitem', { name: 'Maggi Masala Noodles 70 g' })).toHaveTextContent('Added');
    // Leaving the rest out sends only the open line.
    fireEvent.click(screen.getByRole('button', { name: 'Discard the rest' }));
    await waitFor(() => expect(calls.filter((c) => c.path.endsWith('/confirm'))).toHaveLength(2));
    expect(calls.filter((c) => c.path.endsWith('/confirm'))[1]?.body).toEqual({ lines: [{ lineId: 'l2', outcome: 'discarded' }] });
  });

  it('is an empty state when the feature is off (SC-AI-24)', async () => {
    renderAt('/onboarding/shelf', { messages, handlers: [shops('owner'), features({ shelfPhoto: false })] });
    expect(await screen.findByText('Shelf photo is not on for your shop')).toBeInTheDocument();
  });
});

const REVIEWS = '/v1/biz/businesses/b1/stores/s1/reviews';
const reviewList: Handler = (c) =>
  c.method === 'GET' && c.path === REVIEWS
    ? json(200, {
        items: [{ id: 'r1', stars: 4, text: 'Good stock', tags: [], createdAt: '2026-10-20T08:00:00.000Z', editedAt: null, customerName: 'Asha', reply: null, reported: false }],
        nextCursor: null,
        rating: { avg: 4, count: 10 },
        serverNow: '2026-10-24T08:00:00.000Z',
      })
    : undefined;

function replyApi(calls: Call[]): Handler {
  return (c) => {
    if (c.path.startsWith(`${AI}/review-reply/drafts`)) {
      calls.push(c);
      if (c.method === 'POST' && c.path.endsWith('/drafts')) return json(202, { draftId: 'd3' });
      if (c.method === 'GET') return json(200, { ...base, draftId: 'd3', feature: 'review-reply', status: 'ready', reviewId: 'r1', lines: [{ lineId: 'l1', text: 'Thank you, Asha!', language: 'en' }] });
      return json(200, { draftId: 'd3', status: 'confirmed', lines: [{ lineId: 'l1', outcome: 'edited' }] });
    }
    if (c.path === `${REVIEWS}/r1/reply`) calls.push(c);
    return undefined;
  };
}

describe('B-11 reply draft (AI-R5)', () => {
  it('fills the box with a draft and posts the person\'s words through the confirm only (SC-AI-19, SC-AI-20)', async () => {
    const calls: Call[] = [];
    renderAt('/reviews', { messages, handlers: [shops('owner'), features(), reviewList, replyApi(calls)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reply' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Draft a reply' }));
    const box = screen.getByLabelText('Your reply');
    await waitFor(() => expect(box).toHaveValue('Thank you, Asha!'), { timeout: 4000 });
    expect(screen.getByText(/Drafted by AI/)).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST' && c.path.endsWith('/drafts'))?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(calls.some((c) => c.path.endsWith('/confirm'))).toBe(false);
    fireEvent.change(box, { target: { value: 'Thank you, Asha! See you soon.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('Reply sent')).toBeInTheDocument();
    const confirm = calls.find((c) => c.path.endsWith('/confirm'));
    expect(confirm?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(confirm?.body).toEqual({ lines: [{ lineId: 'l1', outcome: 'edited', value: { text: 'Thank you, Asha! See you soon.' } }] });
    expect(calls.some((c) => c.path === `${REVIEWS}/r1/reply`)).toBe(false);
  });

  it('has no draft button when the feature is off (SC-AI-24)', async () => {
    renderAt('/reviews', { messages, handlers: [shops('owner'), features({ reviewReply: false }), reviewList] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reply' }));
    expect(await screen.findByLabelText('Your reply')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Draft a reply' })).not.toBeInTheDocument();
  });
});

const OFFERS = '/v1/biz/businesses/b1/stores/s1';
const liveOffer = {
  id: 'o1', storeId: 's1', title: 'Diwali 20% off', kind: 'percent', value: 20, buyQty: null, getQty: null, minBillPaise: null, scope: 'items',
  storeItemIds: ['i1'], categoryId: null, channels: 'both', perCustomerLimit: null, maxRedemptions: 50, redemptions: 7,
  startsAt: '2026-10-20T08:00:00.000Z', endsAt: null, status: 'live', source: 'manual', batchId: null, version: 3,
  createdAt: '2026-10-20T08:00:00.000Z', updatedAt: '2026-10-20T08:00:00.000Z',
};
const offersApi: Handler = (c) => {
  if (c.method === 'GET' && c.path === `${OFFERS}/offers`) return json(200, { items: [liveOffer], nextCursor: null });
  if (c.method === 'GET' && c.path === `${OFFERS}/banners`) return json(200, { items: [], nextCursor: null });
  return undefined;
};

function copyApi(calls: Call[]): Handler {
  return (c) => {
    if (!c.path.startsWith(`${AI}/banner-copy/drafts`)) return undefined;
    calls.push(c);
    if (c.method === 'POST' && c.path.endsWith('/drafts')) return json(202, { draftId: 'd4' });
    if (c.method === 'GET')
      return json(200, {
        ...base, draftId: 'd4', feature: 'banner-copy', status: 'ready', language: 'en',
        lines: [
          { lineId: 'l1', headline: 'Diwali treats are in', subline: 'Pick up today', headlineEn: 'Diwali treats are in', sublineEn: 'Pick up today', tint: 'amber', storeItemId: null },
          { lineId: 'l2', headline: 'Light up your Diwali', subline: '', headlineEn: 'Light up your Diwali', sublineEn: '', tint: 'rose', storeItemId: null },
        ],
      });
    return json(200, { draftId: 'd4', status: 'confirmed', lines: [], copy: { headline: 'Light up your Diwali', subline: '', tint: 'rose' } });
  };
}

async function openBannerSheet() {
  fireEvent.click(await screen.findByRole('tab', { name: 'Banners' }));
  fireEvent.click((await screen.findAllByRole('button', { name: 'New banner' }))[0] as HTMLElement);
  const sheet = await screen.findByRole('dialog');
  fireEvent.click(within(sheet).getByRole('radio', { name: 'An offer' }));
  fireEvent.change(within(sheet).getByLabelText(/offer/i, { selector: 'select' }), { target: { value: 'o1' } });
  return sheet;
}

describe('B-08 banner wording (AI-R9)', () => {
  it('fills the fields with the picked option and creates no banner itself (SC-AI-21, SC-AI-22)', async () => {
    const calls: Call[] = [];
    const posts: Call[] = [];
    renderAt('/offers', { messages, handlers: [shops('manager'), features(), offersApi, copyApi(calls), (c) => (c.method === 'POST' && c.path === `${OFFERS}/banners` ? (posts.push(c), undefined) : undefined)] });
    const sheet = await openBannerSheet();
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Suggest wording' }));
    const options = await within(sheet).findByRole('group', { name: 'Pick a wording' }, { timeout: 4000 });
    const create = calls.find((c) => c.method === 'POST' && c.path.endsWith('/drafts'));
    expect(create?.body).toEqual({ offerId: 'o1' });
    expect(create?.headers.get('Idempotency-Key')).toBeTruthy();
    fireEvent.click(within(options).getByRole('radio', { name: /Light up your Diwali/ }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Use this' }));
    await waitFor(() => expect(within(sheet).getByLabelText('Headline')).toHaveValue('Light up your Diwali'));
    const confirm = calls.find((c) => c.path.endsWith('/confirm'));
    expect(confirm?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(confirm?.body).toEqual({ lines: [{ lineId: 'l1', outcome: 'discarded' }, { lineId: 'l2', outcome: 'accepted' }] });
    expect(posts).toHaveLength(0);
  });

  it('offers no suggestion when the feature is off (SC-AI-24)', async () => {
    renderAt('/offers', { messages, handlers: [shops('manager'), features({ bannerCopy: false }), offersApi] });
    const sheet = await openBannerSheet();
    expect(within(sheet).getByLabelText('Headline')).toBeInTheDocument();
    expect(within(sheet).queryByRole('button', { name: 'Suggest wording' })).not.toBeInTheDocument();
  });
});

describe('AI-R4 insight text', () => {
  it('marks a model-worded insight sentence (SC-AI-17)', async () => {
    const worded = INSIGHTS.map((i, n) => (n === 0 ? { ...i, text: 'Amul Taaza 500 ml sold 96 this week and runs out tomorrow.' } : i));
    const handlers = ownerApi();
    handlers.splice(2, 1, (c) => (c.path === `${STORE}/insights` && c.method === 'GET' ? json(200, { items: worded, nextCursor: null, period: { from: '2025-12-29', to: '2026-01-05' }, serverNow: '2026-01-05T13:10:00.000Z' }) : undefined));
    renderAt('/', { messages, handlers });
    expect(await screen.findByText(/sold 96 this week/)).toBeInTheDocument();
    expect(screen.getAllByText('Worded by AI')).toHaveLength(1);
  });
});

describe('the decisions the client sends', () => {
  it('bill: unticked and unmatched are discarded, unchanged is accepted, changed is edited', () => {
    const edits = Object.fromEntries(billLines.map((l) => [l.lineId, startEdit(l)]));
    expect(decide(billLines, edits).decisions.map((d) => d.outcome)).toEqual(['accepted', 'accepted', 'discarded']);
    const off = { ...edits, l1: { ...at(edits, 'l1'), on: false }, l2: { ...at(edits, 'l2'), cost: '' } };
    expect(decide(billLines, off).decisions).toEqual([
      { lineId: 'l1', outcome: 'discarded' },
      { lineId: 'l2', outcome: 'edited', value: { storeItemId: 'si2', quantity: 12, costPaise: null, expiresOn: null } },
      { lineId: 'l3', outcome: 'discarded' },
    ]);
    expect(decide(billLines, { ...edits, l2: { ...at(edits, 'l2'), cost: 'abc' } }).problems).toEqual({ l2: 'cost' });
  });

  it('shelf: a listed product is never sent as an add; a bad price is a problem', () => {
    const edits = Object.fromEntries(shelfLines.map((l) => [l.lineId, startShelf(l)]));
    expect(decideShelf(shelfLines, edits).decisions.map((d) => d.outcome)).toEqual(['accepted', 'accepted', 'discarded']);
    expect(decideShelf(shelfLines, { ...edits, l1: { ...at(edits, 'l1'), price: '' } }).problems).toEqual({ l1: 'price' });
  });

  it('reply: the drafted text as is is accepted, anything else is edited', () => {
    expect(replyDecision('l1', ' Thanks! ', 'Thanks!')).toEqual({ lineId: 'l1', outcome: 'accepted' });
    expect(replyDecision('l1', 'Thanks!', 'Thanks a lot!')).toEqual({ lineId: 'l1', outcome: 'edited', value: { text: 'Thanks a lot!' } });
  });

  it('banner: one pick is accepted, no pick discards all', () => {
    const d = { lines: [{ lineId: 'l1' }, { lineId: 'l2' }] } as Parameters<typeof bannerDecisions>[0];
    expect(bannerDecisions(d, 'l2').map((x) => x.outcome)).toEqual(['discarded', 'accepted']);
    expect(bannerDecisions(d, null).map((x) => x.outcome)).toEqual(['discarded', 'discarded']);
  });

  it('photo: type before size', () => {
    expect(checkPhoto(photo('image/gif'), ON.photoTypes, MB)).toBe('type');
    expect(checkPhoto(photo('image/png', 2 * MB), ON.photoTypes, MB)).toBe('size');
    expect(checkPhoto(photo(), ON.photoTypes, MB)).toBeNull();
  });
});

describe('AI draft loading states', () => {
  it('Discard draft shows its spinner, and Add stock waits, while the call runs', async () => {
    const calls: Call[] = [];
    renderAt('/restock-scan', { messages, handlers: [shops('owner'), features(), holding((c) => c.method === 'POST' && c.path.endsWith('/confirm')), bill(calls)] });
    await pick(photo());
    await screen.findByRole('list', { name: 'Bill lines' }, { timeout: 4000 });
    const discard = screen.getByRole('button', { name: 'Discard draft' });
    fireEvent.click(discard);
    await waitFor(() => expect(discard).toHaveAttribute('aria-busy', 'true'));
    const add = screen.getByRole('button', { name: /^Add stock/ });
    expect(add).not.toHaveAttribute('aria-busy');
    expect(add).toBeDisabled();
  });

  it('banner wording: None of these shows its spinner, and Use this waits, while the call runs', async () => {
    const calls: Call[] = [];
    renderAt('/offers', { messages, handlers: [shops('manager'), features(), offersApi, holding((c) => c.method === 'POST' && c.path.endsWith('/confirm')), copyApi(calls)] });
    const sheet = await openBannerSheet();
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Suggest wording' }));
    await within(sheet).findByRole('group', { name: 'Pick a wording' }, { timeout: 4000 });
    const none = within(sheet).getByRole('button', { name: 'None of these' });
    fireEvent.click(none);
    await waitFor(() => expect(none).toHaveAttribute('aria-busy', 'true'));
    const use = within(sheet).getByRole('button', { name: 'Use this' });
    expect(use).not.toHaveAttribute('aria-busy');
    expect(use).toBeDisabled();
  });
});
