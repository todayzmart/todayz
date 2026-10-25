import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../../test/render';

const messages = asMessages(en);
const B = '/v1/biz/businesses/b1';
const LIST = `${B}/stores/s1/standees`;

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
      : undefined;
}
const store: Handler = (c) => (c.method === 'GET' && c.path === `${B}/stores/s1` ? json(200, { id: 's1', shortCode: 'K7Q2X9' }) : undefined);

function standee(id: string, campaign: string, counts: { scans: number; signUps: number; qualified: number }, extra: Record<string, unknown> = {}) {
  return {
    id,
    campaign,
    link: `https://api.test/v1/public/ref/K7Q2X9?s=${id}`,
    counts,
    lastScanAt: counts.scans > 0 ? '2026-10-24T10:00:00Z' : null,
    reorderRequestedAt: null as string | null,
    printedAt: null,
    createdAt: '2026-10-20T10:00:00Z',
    version: 1,
    ...extra,
  };
}

function listOf(items: ReturnType<typeof standee>[], limit = 20) {
  const totals = items.reduce((t, s) => ({ scans: t.scans + s.counts.scans, signUps: t.signUps + s.counts.signUps, qualified: t.qualified + s.counts.qualified }), { scans: 0, signUps: 0, qualified: 0 });
  return { items, totals, limit };
}

const card = async () => within((await screen.findByRole('heading', { name: 'Campaigns' })).closest('section') as HTMLElement);

describe('standee campaigns (B-10, W3-09)', () => {
  it('shows each standee with its scans, sign-ups and qualified counts, and the totals', async () => {
    const items = [standee('st1', 'Counter', { scans: 12, signUps: 4, qualified: 1 }), standee('st2', 'Diwali gate', { scans: 0, signUps: 0, qualified: 0 })];
    renderAt('/standee', { messages, handlers: [shops('owner'), store, (c) => (c.method === 'GET' && c.path === LIST ? json(200, listOf(items)) : undefined)] });
    const c = await card();
    const totals = await c.findByLabelText('All standees');
    expect(within(totals).getByText('12')).toBeInTheDocument();
    expect(within(totals).getByText('4')).toBeInTheDocument();
    const counter = c.getByLabelText('Counter counts');
    expect(within(counter).getByText('Scans')).toBeInTheDocument();
    expect(within(counter).getByText('12')).toBeInTheDocument();
    expect(c.getByText('Not scanned yet')).toBeInTheDocument();
    expect(c.getByText(/^Last scan/)).toBeInTheDocument();
  });

  it('shows the empty state when the shop has no standees', async () => {
    renderAt('/standee', { messages, handlers: [shops('manager'), store, (c) => (c.path === LIST ? json(200, listOf([])) : undefined)] });
    const c = await card();
    expect(await c.findByText('No standees yet')).toBeInTheDocument();
    expect(c.queryByLabelText('All standees')).toBeNull();
  });

  it('adds a standee with an Idempotency-Key and asks for a name first', async () => {
    let items = [standee('st1', 'Counter', { scans: 1, signUps: 0, qualified: 0 })];
    const { calls } = renderAt('/standee', {
      messages,
      handlers: [
        shops('owner'),
        store,
        (c) => {
          if (c.path !== LIST) return undefined;
          if (c.method === 'POST') {
            const made = standee('st2', (c.body as { campaign: string }).campaign, { scans: 0, signUps: 0, qualified: 0 });
            items = [made, ...items];
            return json(201, made);
          }
          return json(200, listOf(items));
        },
      ],
    });
    const c = await card();
    fireEvent.click(await c.findByRole('button', { name: 'Add standee' }));
    expect(await c.findByText('Give the standee a campaign name')).toBeInTheDocument();
    expect(calls.some((x) => x.method === 'POST' && x.path === LIST)).toBe(false);

    fireEvent.change(c.getByLabelText('Campaign name'), { target: { value: '  Diwali gate ' } });
    fireEvent.click(c.getByRole('button', { name: 'Add standee' }));
    expect(await c.findByText('Diwali gate')).toBeInTheDocument();
    const post = calls.find((x) => x.method === 'POST' && x.path === LIST);
    if (!post) throw new Error('no create request');
    expect(post.body).toEqual({ campaign: 'Diwali gate' });
    expect(post.headers.get('Idempotency-Key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByText('Standee added')).toBeInTheDocument();
  });

  it('says so when the server refuses a new standee at the limit', async () => {
    renderAt('/standee', {
      messages,
      handlers: [shops('owner'), store, (c) => (c.path !== LIST ? undefined : c.method === 'POST' ? problem(409, 'standee_limit_reached') : json(200, listOf([])))],
    });
    const c = await card();
    fireEvent.change(await c.findByLabelText('Campaign name'), { target: { value: 'Gate' } });
    fireEvent.click(c.getByRole('button', { name: 'Add standee' }));
    expect(await c.findByText('This shop already has as many standees as it can have.')).toBeInTheDocument();
  });

  it('hides the form once the shop is at its limit', async () => {
    const items = [standee('st1', 'Counter', { scans: 0, signUps: 0, qualified: 0 }), standee('st2', 'Gate', { scans: 0, signUps: 0, qualified: 0 })];
    renderAt('/standee', { messages, handlers: [shops('owner'), store, (c) => (c.path === LIST ? json(200, listOf(items, 2)) : undefined)] });
    const c = await card();
    expect(await c.findByText('This shop has 2 standees, the most it can have.')).toBeInTheDocument();
    expect(c.queryByRole('button', { name: 'Add standee' })).toBeNull();
  });

  it('requests a reprint and shows it as waiting after the refetch', async () => {
    let item = standee('st1', 'Counter', { scans: 3, signUps: 1, qualified: 0 });
    const { calls } = renderAt('/standee', {
      messages,
      handlers: [
        shops('manager'),
        store,
        (c) => {
          if (c.method === 'POST' && c.path === `${LIST}/st1/reorder`) {
            item = { ...item, reorderRequestedAt: '2026-10-25T09:00:00Z', version: 2 };
            return json(200, item);
          }
          return c.method === 'GET' && c.path === LIST ? json(200, listOf([item])) : undefined;
        },
      ],
    });
    const c = await card();
    fireEvent.click(await c.findByRole('button', { name: 'Reorder printed standee' }));
    expect(await c.findByText('Reorder requested')).toBeInTheDocument();
    await waitFor(() => expect(c.queryByRole('button', { name: 'Reorder printed standee' })).toBeNull());
    const post = calls.find((x) => x.method === 'POST' && x.path.endsWith('/reorder'));
    expect(post?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-Z]{26}$/);
  });

  it('a cashier never asks for the standees', async () => {
    const { calls } = renderAt('/standee', { messages, handlers: [shops('cashier'), store] });
    expect(await screen.findByText('The standee is for owners and managers')).toBeInTheDocument();
    expect(calls.some((c) => c.path === LIST)).toBe(false);
  });
});
