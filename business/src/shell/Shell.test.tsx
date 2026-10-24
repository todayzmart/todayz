import en from '@todayz/i18n/compiled/business/en.json';
import xa from '@todayz/i18n/compiled/business/en-XA.json';
import { asMessages, type Messages } from '@todayz/i18n';
import { screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, renderAt as renderApp, type Call, type Handler } from '../test/render';
import { MORE, NAV } from './nav';
import { NARROW_QUERY } from './useNarrow';

function renderAt(path: string, messages: Messages = asMessages(en), handlers: Handler[] = []) {
  return renderApp(path, { messages, handlers });
}

const BASE = '/v1/biz/businesses/b1/stores/s1';

function shops(c: Call) {
  return c.path === '/v1/biz/businesses'
    ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'owner', stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
    : undefined;
}

/** The New list with `n` orders; the other tabs are empty. */
function newOrders(n: number): Handler {
  return (c) => {
    if (c.method !== 'GET' || c.path !== `${BASE}/orders`) return undefined;
    const isNew = (new URLSearchParams(c.search).get('status') ?? 'new') === 'new';
    const items = isNew ? Array.from({ length: n }, (_, i) => ({ id: `o${i}`, number: `TZ-${i}`, customerName: 'Meera', fulfilment: 'pickup', status: 'placed', statusReason: null, itemCount: 1, totalPaise: 5800, acceptBy: '2026-10-23T12:34:30.000Z', holdUntil: null, placedAt: '2026-10-23T12:29:30.000Z', seen: true, version: 1 })) : [];
    return json(200, { items, nextCursor: null, serverNow: '2026-10-23T12:30:00.000Z' });
  };
}

function mockWidth(width: number) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === NARROW_QUERY ? width < 1024 : false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }));
}

afterEach(() => vi.unstubAllGlobals());

/** [sidebar, bottom tabs]: both are in the DOM; CSS shows one per breakpoint. */
async function navs() {
  const all = await screen.findAllByRole('navigation', { name: 'Business' });
  const side = all.find((n) => n.classList.contains('tz-side'));
  const tabs = all.find((n) => n.classList.contains('tz-bnav'));
  if (!side || !tabs) throw new Error('missing sidebar or tabs');
  return { side, tabs };
}

describe('business shell', () => {
  it('lands on Overview at / (B-03)', async () => {
    const { history } = renderAt('/');
    expect(await screen.findByRole('heading', { level: 1, name: /^Good (morning|afternoon|evening), / })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/');
    const { tabs, side } = await navs();
    expect(within(tabs).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
    expect(within(side).getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
    expect(within(side).getByRole('link', { name: /^Orders\b/ })).not.toHaveAttribute('aria-current');
  });

  it('counts the live New orders on Orders in the sidebar only, read out in words', async () => {
    // The live stream uses the global fetch; it falls back to polling here.
    vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
    renderAt('/orders', asMessages(en), [shops, newOrders(2)]);
    const { side, tabs } = await navs();
    const orders = await within(side).findByRole('link', { name: /2 orders waiting$/ });
    expect(orders).toHaveAccessibleName(/^Orders\W+2 orders waiting$/);
    expect(orders.querySelector('.tz-count')).toHaveTextContent('2');
    expect(orders.querySelector('.tz-count')).toHaveAttribute('aria-hidden', 'true');
    // Stock's number comes from the Overview, which still runs on sample data: no badge.
    expect(within(side).getByRole('link', { name: 'Stock' }).querySelector('.tz-count')).toBeNull();
    expect(side.querySelectorAll('.tz-count')).toHaveLength(1);
    expect(tabs.querySelectorAll('.tz-count')).toHaveLength(0);
  });

  it('links a built section to its own route, not the /$section template (no router warning)', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    renderAt('/');
    const { side } = await navs();
    expect(within(side).getByRole('link', { name: /^Orders\b/ })).toHaveAttribute('href', '/orders');
    expect(warn.mock.calls.flat().join(' ')).not.toMatch(/Generated path/);
    warn.mockRestore();
  });

  it('keeps Overview active only on / (not on every page)', async () => {
    renderAt('/orders');
    const { side } = await navs();
    expect(within(side).getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current');
    expect(within(side).getByRole('link', { name: /^Orders\b/ })).toHaveAttribute('aria-current', 'page');
  });

  it('replaces /overview with / and keeps the query string', async () => {
    const { history } = renderAt('/overview?locale=pseudo');
    await screen.findByRole('heading', { level: 1, name: /^Good / });
    expect(history.location.pathname).toBe('/');
    expect(history.location.search).toBe('?locale=pseudo');
    expect(history.length).toBe(1);
  });

  it('has phone tabs Overview, Orders, Sell, Stock, More', async () => {
    renderAt('/orders');
    const { tabs } = await navs();
    expect(within(tabs).getAllByRole('link').map((l) => l.textContent)).toEqual(['Overview', 'Orders', 'Sell', 'Stock', 'More']);
  });

  it('gives the content area touch density below 1024px only', async () => {
    mockWidth(375);
    const { unmount } = renderAt('/orders');
    await navs();
    expect(screen.getByRole('main')).toHaveAttribute('data-density', 'touch');
    unmount();
    mockWidth(1280);
    renderAt('/orders');
    await navs();
    expect(screen.getByRole('main')).not.toHaveAttribute('data-density');
  });

  it('has the sidebar with the Business tag and the Grow and Shop groups', async () => {
    renderAt('/');
    const { side } = await navs();
    expect(within(side).getByText('Business')).toHaveClass('tz-side-tag');
    expect(within(side).getByText('Grow')).toHaveClass('tz-side-group');
    expect(within(side).getByText('Shop')).toHaveClass('tz-side-group');
    expect(within(side).queryByRole('link', { name: 'More' })).toBeNull();
    // Nav items only: the setup card stays hidden while the Overview runs on sample data.
    await screen.findByRole('heading', { level: 1, name: /^Good / });
    expect(side.querySelector('.tz-side-card')).toBeNull();
    expect(within(side).getAllByRole('link')).toHaveLength(NAV.filter((n) => n.side).length);
  });

  // Sections that have a real page (orders: W2-10; stock, offers: W2-12) no longer show the placeholder.
  const BUILT = new Set(['more', 'overview', 'orders', 'stock', 'offers', 'reviews', 'credits']);
  it.each(NAV.filter((n) => !BUILT.has(n.section)))('shows the placeholder for /$section', async ({ section }) => {
    renderAt(`/${section}`);
    const { side } = await navs();
    const active = within(side).getAllByRole('link').find((l) => l.getAttribute('aria-current') === 'page');
    expect(active?.getAttribute('href')).toBe(`/${section}`);
    expect(screen.getByText('Coming soon')).toBeInTheDocument();
  });

  it('lists the sidebar-only sections under More', async () => {
    renderAt('/more');
    const list = await screen.findByRole('region', { name: 'More' });
    expect(within(list).getAllByRole('link').map((l) => l.getAttribute('href'))).toEqual(MORE.map((m) => `/${m.section}`));
  });

  it('shows not found for unknown paths', async () => {
    renderAt('/no-such-page');
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to overview' })).toHaveAttribute('href', '/');
  });

  it('renders the pseudo-locale with every key present', async () => {
    renderAt('/', asMessages(xa));
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.textContent).toMatch(/^\[.*\]$/);
  });
});
