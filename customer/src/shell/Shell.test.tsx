import en from '@todayz/i18n/compiled/customer/en.json';
import xa from '@todayz/i18n/compiled/customer/en-XA.json';
import { asMessages, type Messages } from '@todayz/i18n';
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderAt as renderApp } from '../test/render';
import { NAV } from './nav';

function renderAt(path: string, messages: Messages = asMessages(en)) {
  return renderApp(path, { messages });
}

describe('customer shell', () => {
  it('renders home with the hero and a five-item bottom nav', async () => {
    renderAt('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeInTheDocument();
    const nav = screen.getByRole('navigation', { name: 'Main' });
    const links = within(nav).getAllByRole('link');
    expect(links.map((l) => l.textContent)).toEqual(['Home', 'Search', 'Orders', 'Wallet', 'Account']);
    expect(within(nav).getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
    expect(within(nav).getByRole('link', { name: 'Search' })).not.toHaveAttribute('aria-current');
  });

  it.each(NAV.filter((n) => n.section !== '' && n.section !== 'account' && n.section !== 'search' && n.section !== 'orders'))('shows the placeholder for /$section', async ({ section }) => {
    renderAt(`/${section}`);
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    const active = within(nav).getAllByRole('link').find((l) => l.getAttribute('aria-current') === 'page');
    expect(active?.getAttribute('href')).toBe(`/${section}`);
    expect(screen.getByText('Coming soon')).toBeInTheDocument();
  });

  it('shows not found for unknown paths', async () => {
    renderAt('/no-such-page');
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to home' })).toHaveAttribute('href', '/');
  });

  it('renders the pseudo-locale with every key present', async () => {
    renderAt('/', asMessages(xa));
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.textContent).toMatch(/^\[.*\]$/);
    expect(h1.textContent).not.toContain('Everything nearby');
  });

  it('has a skip link to the main landmark', async () => {
    renderAt('/');
    const skip = await screen.findByRole('link', { name: 'Skip to content' });
    expect(skip).toHaveAttribute('href', '#main');
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main');
  });
});
