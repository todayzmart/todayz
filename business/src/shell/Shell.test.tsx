import en from '@todayz/i18n/compiled/business/en.json';
import xa from '@todayz/i18n/compiled/business/en-XA.json';
import { asMessages, type Messages } from '@todayz/i18n';
import { createMemoryHistory } from '@tanstack/react-router';
import { render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from '../app/App';
import { makeQueryClient, makeRouter } from '../app/router';
import { MORE, NAV } from './nav';
import { NARROW_QUERY } from './useNarrow';

function renderAt(path: string, messages: Messages = asMessages(en)) {
  const queryClient = makeQueryClient();
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = makeRouter(queryClient, history);
  return { history, ...render(<App messages={messages} router={router} queryClient={queryClient} />) };
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
  it('lands on Orders: / is replaced by /orders (D-84)', async () => {
    const { history } = renderAt('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Orders' })).toBeInTheDocument();
    expect(history.location.pathname).toBe('/orders');
    // Replaced, not pushed: there is no "/" entry to go back to.
    expect(history.length).toBe(1);
    const { tabs, side } = await navs();
    expect(within(tabs).getByRole('link', { name: 'Orders' })).toHaveAttribute('aria-current', 'page');
    expect(within(side).getByRole('link', { name: 'Orders' })).toHaveAttribute('aria-current', 'page');
    expect(within(tabs).getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current');
  });

  it('keeps the query string across the landing redirect', async () => {
    const { history } = renderAt('/?locale=pseudo');
    await screen.findByRole('heading', { level: 1, name: 'Orders' });
    expect(history.location.pathname).toBe('/orders');
    expect(history.location.search).toBe('?locale=pseudo');
  });

  it('keeps Overview as a placeholder with the greeting band', async () => {
    renderAt('/overview');
    expect(await screen.findByRole('heading', { level: 1, name: 'Your shop on TodayZ' })).toBeInTheDocument();
    expect(screen.getByText('Coming soon')).toBeInTheDocument();
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
    expect(within(side).getAllByRole('link')).toHaveLength(NAV.filter((n) => n.side).length);
  });

  it.each(NAV.filter((n) => n.section !== 'more'))('shows the placeholder for /$section', async ({ section }) => {
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
    expect(screen.getByRole('link', { name: 'Go to orders' })).toHaveAttribute('href', '/');
  });

  it('renders the pseudo-locale with every key present', async () => {
    renderAt('/overview', asMessages(xa));
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.textContent).toMatch(/^\[.*\]$/);
  });
});
