import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { cleanReply } from './limits';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

function review(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    stars: 4,
    text: `Text ${id}`,
    tags: ['fresh_stock'],
    createdAt: '2026-10-20T08:00:00.000Z',
    editedAt: null,
    customerName: 'Asha',
    reply: null,
    reported: false,
    ...over,
  };
}

const rating: { avg: number | null; count: number } = { avg: 4.2, count: 10 };

function list(items: unknown[], nextCursor: string | null = null, rate = rating): Handler {
  return (c) => (c.method === 'GET' && c.path === `${BASE}/reviews` ? json(200, { items, nextCursor, rating: rate, serverNow: '2026-10-24T08:00:00.000Z' }) : undefined);
}

describe('B-11 reviews', () => {
  it('shows the summary and the reviews', async () => {
    renderAt('/reviews', { messages, handlers: [shops('owner'), list([review('r1', { tags: ['fresh_stock', 'slow'] })])] });
    expect(await screen.findByText('Text r1')).toBeInTheDocument();
    expect(screen.getByText('10 reviews')).toBeInTheDocument();
    expect(screen.getByText('4.2')).toBeInTheDocument();
    expect(screen.getAllByRole('img', { name: '4 out of 5 stars' }).length).toBeGreaterThan(0);
    expect(screen.getByText('Fresh stock')).toBeInTheDocument();
    expect(screen.getByText('Slow')).toBeInTheDocument();
    expect(screen.getByText(/Asha/)).toBeInTheDocument();
  });

  it('sends a cashier to ask a manager and loads nothing', async () => {
    let called = false;
    renderAt('/reviews', { messages, handlers: [shops('cashier'), (c) => ((called = called || c.path.endsWith('/reviews')), undefined)] });
    expect(await screen.findByText(/Ask a manager/)).toBeInTheDocument();
    expect(called).toBe(false);
  });

  it('replies with POST, then edits with PATCH', async () => {
    const calls: Call[] = [];
    const reply = { text: 'Thanks!', createdAt: '2026-10-21T08:00:00.000Z', editedAt: null };
    renderAt('/reviews', {
      messages,
      handlers: [
        shops('manager'),
        (c) => {
          if (c.path === `${BASE}/reviews/r1/reply`) {
            calls.push(c);
            return c.method === 'POST' ? json(201, reply) : json(200, { ...reply, text: 'Thanks a lot!', editedAt: '2026-10-22T08:00:00.000Z' });
          }
          return undefined;
        },
        list([review('r1')]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Reply' }));
    const box = screen.getByLabelText('Your reply');
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('Write a reply first.')).toBeInTheDocument();
    fireEvent.change(box, { target: { value: '  Thanks!  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
    expect(await screen.findByText('Reply sent')).toBeInTheDocument();
    expect(calls[0]?.method).toBe('POST');
    expect(calls[0]?.body).toEqual({ text: 'Thanks!' });
    expect(await screen.findByText('Thanks!')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Edit reply' }));
    fireEvent.change(screen.getByLabelText('Edit your reply'), { target: { value: 'Thanks a lot!' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save reply' }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1]?.method).toBe('PATCH');
    expect(calls[1]?.body).toEqual({ text: 'Thanks a lot!' });
    expect(await screen.findByText('Thanks a lot!')).toBeInTheDocument();
  });

  it('says the reply can no longer be edited when TodayZ hid it', async () => {
    const reply = { text: 'Hi', createdAt: '2026-10-21T08:00:00.000Z', editedAt: null };
    renderAt('/reviews', {
      messages,
      handlers: [shops('owner'), (c) => (c.method === 'PATCH' ? problem(409, 'invalid_transition') : undefined), list([review('r1', { reply })])],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Edit reply' }));
    fireEvent.change(screen.getByLabelText('Edit your reply'), { target: { value: 'Hello' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save reply' }));
    expect(await screen.findByText('This reply can no longer be edited.')).toBeInTheDocument();
  });

  it('reports a review with a reason, then shows Reported and disables the control', async () => {
    let posted: Call | undefined;
    renderAt('/reviews', {
      messages,
      handlers: [
        shops('owner'),
        (c) => {
          if (c.method === 'POST' && c.path === `${BASE}/reviews/r1/report`) {
            posted = c;
            return json(201, { id: 'rp1', reviewId: 'r1', status: 'open', createdAt: '2026-10-24T08:00:00.000Z' });
          }
          return undefined;
        },
        list([review('r1')]),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Report' }));
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).getByRole('button', { name: 'Report review' })).toHaveAttribute('disabled');
    fireEvent.click(within(sheet).getByLabelText('Spam or advertising'));
    fireEvent.change(within(sheet).getByLabelText('Note (optional)'), { target: { value: ' ad ' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Report review' }));
    expect(await screen.findByText('Review reported to TodayZ')).toBeInTheDocument();
    expect(posted?.body).toEqual({ reason: 'spam: ad' });
    const btn = await screen.findByRole('button', { name: 'Reported' });
    expect(btn).toBeDisabled();
  });

  it('pages with Show more using the cursor', async () => {
    const seen: string[] = [];
    renderAt('/reviews', {
      messages,
      handlers: [
        shops('owner'),
        (c) => {
          if (c.path !== `${BASE}/reviews`) return undefined;
          const cursor = new URLSearchParams(c.search).get('cursor');
          seen.push(cursor ?? '');
          return json(200, cursor ? { items: [review('r2')], nextCursor: null, rating } : { items: [review('r1')], nextCursor: 'c2', rating });
        },
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    expect(await screen.findByText('Text r2')).toBeInTheDocument();
    expect(seen).toEqual(['', 'c2']);
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
  });

  it('narrows the loaded reviews with the filters', async () => {
    const replied = { text: 'Thanks', createdAt: '2026-10-21T08:00:00.000Z', editedAt: null };
    renderAt('/reviews', {
      messages,
      handlers: [shops('owner'), list([review('r1', { reply: replied }), review('r2', { stars: 1, text: null })])],
    });
    expect(await screen.findByText('Text r1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Needs a reply' }));
    expect(screen.queryByText('Text r1')).not.toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'Reviews' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'With text' }));
    expect(screen.getByText('Text r1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Low stars' }));
    expect(screen.queryByText('Text r1')).not.toBeInTheDocument();
  });

  it('shows the empty state', async () => {
    renderAt('/reviews', {
      messages,
      handlers: [shops('owner'), list([], null, { avg: null, count: 0 })],
    });
    expect(await screen.findByText('No reviews yet')).toBeInTheDocument();
  });

  it('shows an error with a retry', async () => {
    renderAt('/reviews', { messages, handlers: [shops('owner'), (c) => (c.path === `${BASE}/reviews` ? problem(500, 'internal') : undefined)] });
    expect(await screen.findByText("Couldn't load reviews", {}, { timeout: 8000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  }, 15000);
});

describe('cleanReply', () => {
  it('trims and rejects empty text', () => {
    expect(cleanReply('  hi ')).toBe('hi');
    expect(cleanReply('   ')).toBeNull();
  });
});
