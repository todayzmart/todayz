import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ownerApi, shops, STORE } from '../test/insightsApi';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);

function card(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    detector: 'missed_orders_peak_hour',
    subjectRef: 'store',
    kind: 'low',
    topic: 'orders',
    title: `3 orders expired yesterday ${id}`,
    body: 'Nobody accepted them within 10 minutes (7–9 pm). Turn on auto-accept for in-stock items?',
    numbers: { count: 3, minutes: 10 },
    action: { type: 'turn_on_auto_accept', payload: { setting: 'auto_accept' } },
    status: 'seen',
    generatedBy: 'rule',
    lang: 'en',
    createdAt: '2026-01-05T00:30:00.000Z',
    validUntil: '2026-01-07T00:30:00.000Z',
    actedAt: null,
    quietUntil: null,
    ...over,
  };
}

const restock = card('c2', {
  kind: 'action',
  topic: 'stock',
  title: 'Amul butter 100 g runs out in 2 days',
  body: 'At the current pace it lasts 2 days. Restock 15.',
  action: { type: 'restock', payload: { storeItemId: 'si1', qty: 15 } },
  generatedBy: 'ai',
});

/** `GET {S}/coach/cards` per status, and the Done / Not useful posts. */
function coach(cards: Record<string, unknown[]>, calls: Call[] = []): Handler {
  return (c) => {
    if (c.path === `${STORE}/coach/cards` && c.method === 'GET') {
      calls.push(c);
      const status = new URLSearchParams(c.search).get('status') ?? 'live';
      return json(200, { items: cards[status] ?? [], nextCursor: null, serverNow: '2026-01-05T13:10:00.000Z' });
    }
    const m = /\/coach\/cards\/([^/]+)\/(done|dismiss)$/.exec(c.path);
    if (m && c.method === 'POST') {
      calls.push(c);
      const base = [...Object.values(cards).flat()].find((x) => (x as { id: string }).id === m[1]) as Record<string, unknown>;
      return m[2] === 'done'
        ? json(200, { ...base, status: 'done', actedAt: '2026-01-05T13:11:00.000Z' })
        : json(200, { ...base, status: 'dismissed', actedAt: '2026-01-05T13:11:00.000Z', quietUntil: '2026-01-12T13:11:00.000Z' });
    }
    return undefined;
  };
}

describe('B-19 shop coach', () => {
  it('shows the newest live cards on Overview, each with its prefilled action and no metric chip', async () => {
    const calls: Call[] = [];
    renderAt('/', { messages, handlers: [coach({ live: [card('c1'), restock] }, calls), ...ownerApi()] });
    const region = await screen.findByRole('region', { name: 'Shop coach' });
    expect(within(region).getAllByRole('listitem')).toHaveLength(2);
    expect(within(region).getByText('Low')).toBeInTheDocument();
    expect(within(region).getByText('Do this')).toBeInTheDocument();
    expect(within(region).getByRole('link', { name: /Turn on auto-accept/ })).toHaveAttribute('href', '/settings');
    expect(within(region).getByRole('link', { name: /Restock/ })).toHaveAttribute('href', '/stock?restock=si1&qty=15');
    expect(within(region).getByText("Worded by AI from your shop's numbers")).toBeInTheDocument();
    expect(region.querySelector('.tz-chip, .tz-metric')).toBeNull();
    expect(within(region).getByRole('link', { name: 'All tips' })).toHaveAttribute('href', '/coach');
    expect(new URLSearchParams(calls[0]?.search).get('status')).toBe('live');
    expect(new URLSearchParams(calls[0]?.search).get('limit')).toBe('3');
  });

  it('renders nothing on Overview when the coach has no card', async () => {
    renderAt('/', { messages, handlers: [coach({ live: [] }), ...ownerApi()] });
    await screen.findByRole('region', { name: 'Demand insights' });
    expect(screen.queryByRole('region', { name: 'Shop coach' })).toBeNull();
  });

  it('Done posts to /done, hides the card and changes nothing else', async () => {
    const calls: Call[] = [];
    renderAt('/coach', { messages, handlers: [coach({ live: [card('c1')] }, calls), shops('manager')] });
    fireEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('Marked done')).toBeInTheDocument();
    const posts = calls.filter((c) => c.method === 'POST');
    expect(posts.map((c) => c.path)).toEqual([`${STORE}/coach/cards/c1/done`]);
    expect(posts[0]?.body).toBeUndefined();
    await waitFor(() => expect(screen.queryByText('3 orders expired yesterday c1')).toBeNull());
  });

  it('Not useful posts to /dismiss and says until when the subject is quiet', async () => {
    const calls: Call[] = [];
    renderAt('/coach', { messages, handlers: [coach({ live: [card('c1')] }, calls), shops('owner')] });
    fireEvent.click(await screen.findByRole('button', { name: 'Not useful' }));
    expect(await screen.findByText('Hidden until 12 Jan')).toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'POST').map((c) => c.path)).toEqual([`${STORE}/coach/cards/c1/dismiss`]);
  });

  it('keeps the card and explains when it changed under us (409)', async () => {
    renderAt('/coach', {
      messages,
      handlers: [
        (c) => (c.method === 'POST' && c.path.endsWith('/done') ? problem(409, 'invalid_transition') : undefined),
        coach({ live: [card('c1')] }),
        shops('owner'),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('This tip has changed. The list is up to date now.')).toBeInTheDocument();
  });

  it('lists the history tabs without Done or Not useful', async () => {
    const calls: Call[] = [];
    const done = card('c3', { status: 'done', actedAt: '2026-01-04T10:00:00.000Z' });
    renderAt('/coach', { messages, handlers: [coach({ live: [], done: [done] }, calls), shops('owner')] });
    expect(await screen.findByText('Nothing from the coach right now')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'Done' }));
    expect(await screen.findByText('Done 4 Jan')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Done' })).toBeNull();
    expect(screen.queryByRole('link', { name: /auto-accept/ })).toBeNull();
    expect(calls.map((c) => new URLSearchParams(c.search).get('status'))).toEqual(['live', 'done']);
  });

  it('sends a cashier to their screens and never calls the coach', async () => {
    const calls: Call[] = [];
    renderAt('/coach', { messages, handlers: [coach({ live: [card('c1')] }, calls), shops('cashier')] });
    expect(await screen.findByRole('link', { name: 'New sale' })).toBeInTheDocument();
    expect(calls).toHaveLength(0);
  });
});
