import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { resetAlerts } from '../orders/alerts';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';
// 18:00 IST.
const SERVER_NOW = '2026-10-23T12:30:00.000Z';

function shops(role: string): Handler {
  return (c) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function ask(id: string, over: Record<string, unknown> = {}) {
  return {
    askId: id,
    itemName: 'Amul Butter 100 g',
    productId: 'p1',
    storeItemId: 'i1',
    quantity: 2,
    needBy: 'today',
    distanceBand: '1_3km',
    sentAt: '2026-10-23T12:28:00.000Z',
    replyBy: '2026-10-23T12:40:00.000Z',
    status: 'open',
    answer: null,
    pricePaise: null,
    readyBy: null,
    repliedAt: null,
    reported: false,
    available: 9,
    listedPricePaise: 5800,
    mrpPaise: 6000,
    ...over,
  };
}

/** GET /asks: `open` for status=open, `done` for status=all (the inbox drops the open ones itself). */
function asksHandler(lists: { open?: unknown[]; done?: unknown[]; nextCursor?: string | null }): Handler {
  return (c) => {
    if (c.method !== 'GET' || c.path !== `${BASE}/asks`) return undefined;
    const p = new URLSearchParams(c.search);
    const open = lists.open ?? [];
    if (p.get('status') === 'open') return json(200, { asks: open, openCount: open.length, nextCursor: null, serverNow: SERVER_NOW });
    const cursor = p.get('cursor');
    const done = cursor ? [ask('old2', { status: 'missed', sentAt: '2026-10-20T06:00:00.000Z' })] : (lists.done ?? []);
    return json(200, { asks: done, openCount: open.length, nextCursor: cursor ? null : (lists.nextCursor ?? null), serverNow: SERVER_NOW });
  };
}

function settings(muted: boolean): Handler {
  return (c) => (c.method === 'GET' && c.path === `${BASE}/settings` ? json(200, { asksMuted: muted }, { etag: '"1"' }) : undefined);
}

function replyHandler(answer: (c: Call) => Response): Handler {
  return (c) => (c.method === 'POST' && c.path === `${BASE}/asks/a1/reply` ? answer(c) : undefined);
}

const replied = (c: Call) => {
  const body = c.body as { answer: string; pricePaise?: number };
  return json(200, ask('a1', { status: 'replied', answer: body.answer, pricePaise: body.answer === 'yes' ? (body.pricePaise ?? 5800) : null, repliedAt: SERVER_NOW }));
};

async function openReply(item = 'Amul Butter 100 g') {
  fireEvent.click(await screen.findByRole('button', { name: `Reply to the ask for ${item}` }));
  return screen.findByRole('dialog');
}

beforeEach(() => {
  resetAlerts();
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
});
afterEach(() => vi.unstubAllGlobals());

describe('B-20 asks inbox', () => {
  it('opens on the Asks tab from ?tab=asks, counts open asks and counts down from the server clock', async () => {
    renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({ open: [ask('a1'), ask('a2', { itemName: 'Toor dal 1 kg', needBy: null, distanceBand: 'lt_1km' })] })] });
    const tabs = await screen.findByRole('tablist', { name: 'Order status' });
    const asksTab = within(tabs).getByRole('tab', { name: /Asks/ });
    expect(asksTab).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(asksTab).toHaveTextContent('2'));
    const timers = await screen.findAllByTestId('ask-countdown');
    // replyBy is 10 minutes after serverNow.
    expect(timers[0]).toHaveTextContent(/(10:00|9:5\d)/);
    expect(screen.getByText('2 wanted · Need it today · 1–3 km')).toBeInTheDocument();
    expect(screen.getByText('2 wanted · Under 1 km')).toBeInTheDocument();
    // Never a customer's name or phone: the card shows only the ask.
    expect(screen.queryByText(/Meera|\+91/)).not.toBeInTheDocument();
  });

  it('shows the empty state with no asks', async () => {
    renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({})] });
    expect(await screen.findByText('No asks right now')).toBeInTheDocument();
  });

  it('shows the muted note with a link to Settings for a manager when asks are off', async () => {
    renderAt('/orders?tab=asks', { messages, handlers: [shops('manager'), settings(true), asksHandler({})] });
    expect(await screen.findByText('Asks are off for this shop')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open settings' })).toHaveAttribute('href', '/settings');
    expect(screen.queryByText('No asks right now')).not.toBeInTheDocument();
  });

  it('a cashier says yes at the locked listed price with an Idempotency-Key and no price sent', async () => {
    const view = renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({ open: [ask('a1')] }), replyHandler(replied)] });
    const sheet = await openReply();
    expect(within(sheet).getByText('₹58')).toBeInTheDocument();
    expect(within(sheet).getByText(/Your listed price/)).toBeInTheDocument();
    expect(within(sheet).queryByRole('textbox', { name: /Price for one/ })).not.toBeInTheDocument();
    // A cashier cannot add an item to the stock.
    expect(within(sheet).queryByRole('link', { name: 'Add it to your stock' })).not.toBeInTheDocument();
    fireEvent.click(within(sheet).getByRole('button', { name: 'Yes, I have it' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    const post = view.calls.find((c) => c.method === 'POST' && c.path === `${BASE}/asks/a1/reply`);
    expect(post?.body).toEqual({ answer: 'yes' });
    expect(post?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
  });

  it('a manager can change the price, but never above the MRP', async () => {
    const view = renderAt('/orders?tab=asks', { messages, handlers: [shops('manager'), settings(false), asksHandler({ open: [ask('a1')] }), replyHandler(replied)] });
    const sheet = await openReply();
    const price = within(sheet).getByRole('textbox', { name: /Price for one/ });
    expect(price).toHaveValue('58');
    fireEvent.change(price, { target: { value: '65' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Yes, I have it' }));
    expect(await within(sheet).findByRole('alert')).toHaveTextContent(/MRP \(₹60\)/);
    expect(view.calls.some((c) => c.method === 'POST')).toBe(false);
    fireEvent.change(price, { target: { value: '55' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Yes, I have it' }));
    await waitFor(() => expect(view.calls.find((c) => c.method === 'POST')?.body).toEqual({ answer: 'yes', pricePaise: 5500 }));
  });

  it('sends a ready-by time on today in IST', async () => {
    const view = renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({ open: [ask('a1')] }), replyHandler(replied)] });
    const sheet = await openReply();
    // needBy today: no day picker.
    expect(within(sheet).queryByRole('radiogroup', { name: 'Day' })).not.toBeInTheDocument();
    fireEvent.change(within(sheet).getByLabelText('Time'), { target: { value: '20:00' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Yes, I have it' }));
    await waitFor(() => expect(view.calls.find((c) => c.method === 'POST')?.body).toEqual({ answer: 'yes', readyBy: '2026-10-23T14:30:00.000Z' }));
  });

  it('refuses a ready-by time that has passed', async () => {
    const view = renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({ open: [ask('a1')] }), replyHandler(replied)] });
    const sheet = await openReply();
    fireEvent.change(within(sheet).getByLabelText('Time'), { target: { value: '09:00' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Yes, I have it' }));
    expect(await within(sheet).findByRole('alert')).toHaveTextContent("Pick a time that hasn't passed yet");
    expect(view.calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('shows "You have {n}" when the stock is short', async () => {
    renderAt('/orders?tab=asks', { messages, handlers: [shops('cashier'), asksHandler({ open: [ask('a1', { quantity: 3, available: 1 })] })] });
    expect(await screen.findByText('You have 1')).toBeInTheDocument();
  });

  it('offers "Add it to your stock" to a manager on an item the shop does not list', async () => {
    renderAt('/orders?tab=asks', {
      messages,
      handlers: [shops('owner'), settings(false), asksHandler({ open: [ask('a1', { storeItemId: null, listedPricePaise: null, available: null, mrpPaise: null })] })],
    });
    const sheet = await openReply();
    expect(within(sheet).getByRole('textbox', { name: /Price for one/ })).toHaveValue('');
    expect(within(sheet).getByRole('link', { name: 'Add it to your stock' })).toHaveAttribute('href', '/stock?add=p1');
  });

  it('says no, and an ask that closed meanwhile shows why', async () => {
    let closed = false;
    const view = renderAt('/orders?tab=asks', {
      messages,
      handlers: [shops('cashier'), asksHandler({ open: [ask('a1')] }), replyHandler((c) => (closed ? problem(409, 'ask_closed') : replied(c)))],
    });
    closed = true;
    const sheet = await openReply();
    fireEvent.click(within(sheet).getByRole('button', { name: 'No' }));
    expect(await within(sheet).findByRole('alert')).toHaveTextContent('This ask has closed');
    closed = false;
    fireEvent.click(within(sheet).getByRole('button', { name: 'No' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    const posts = view.calls.filter((c) => c.method === 'POST');
    expect(posts.map((c) => c.body)).toEqual([{ answer: 'no' }, { answer: 'no' }]);
    // The retry after a refusal keeps the same key; the server answered, so nothing was sent twice.
    expect(posts[0]?.headers.get('Idempotency-Key')).toBe(posts[1]?.headers.get('Idempotency-Key'));
  });

  it('reports an ask with a reason; nothing is preselected', async () => {
    const view = renderAt('/orders?tab=asks', {
      messages,
      handlers: [shops('cashier'), asksHandler({ open: [ask('a1')] }), (c) => (c.method === 'POST' && c.path === `${BASE}/asks/a1/report` ? new Response(null, { status: 204 }) : undefined)],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Report the ask for Amul Butter 100 g' }));
    const sheet = await screen.findByRole('dialog');
    const confirm = within(sheet).getByRole('button', { name: 'Report' });
    expect(confirm).toBeDisabled();
    within(sheet).getAllByRole('radio').forEach((r) => expect(r).not.toBeChecked());
    fireEvent.click(within(sheet).getByRole('radio', { name: 'Spam' }));
    fireEvent.click(confirm);
    await waitFor(() => expect(view.calls.find((c) => c.method === 'POST')?.body).toEqual({ reason: 'spam' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('lists today\'s finished asks under Done and older ones behind "Show earlier asks"', async () => {
    renderAt('/orders?tab=asks', {
      messages,
      handlers: [
        shops('cashier'),
        asksHandler({
          done: [
            ask('d1', { itemName: 'Curd 400 g', status: 'replied', answer: 'yes', pricePaise: 4500 }),
            ask('d2', { itemName: 'Paneer 200 g', status: 'replied', answer: 'no', reported: true }),
            ask('old1', { itemName: 'Ghee 1 l', status: 'missed', sentAt: '2026-10-22T06:00:00.000Z' }),
          ],
          nextCursor: 'c2',
        }),
      ],
    });
    const done = await screen.findByRole('region', { name: 'Done today' });
    expect(within(done).getByText('Curd 400 g')).toBeInTheDocument();
    expect(within(done).getByText('You said yes')).toBeInTheDocument();
    expect(within(done).getByText('₹45')).toBeInTheDocument();
    expect(within(done).getByText('You said no')).toBeInTheDocument();
    expect(within(done).getByText('Reported')).toBeInTheDocument();
    expect(within(done).queryByText('Ghee 1 l')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show earlier asks' }));
    const earlier = screen.getByRole('region', { name: 'Earlier' });
    expect(within(earlier).getByText('Ghee 1 l')).toBeInTheDocument();
    expect(within(earlier).getByText('Missed')).toBeInTheDocument();
    fireEvent.click(within(earlier).getByRole('button', { name: 'Load more' }));
    await waitFor(() => expect(within(earlier).getAllByText('Missed')).toHaveLength(2));
  });
});
