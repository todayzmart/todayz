import en from '@todayz/i18n/compiled/admin/en.json';
import xa from '@todayz/i18n/compiled/admin/en-XA.json';
import { asMessages, type Messages } from '@todayz/i18n';
import { createMemoryHistory } from '@tanstack/react-router';
import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from '../app/App';
import { makeQueryClient, makeRouter } from '../app/router';
import { NAV } from './nav';

function renderAt(path: string, messages: Messages = asMessages(en)) {
  const queryClient = makeQueryClient();
  const router = makeRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  return render(<App messages={messages} router={router} queryClient={queryClient} />);
}

describe('admin shell', () => {
  it('renders Pulse with the Admin-tagged sidebar', async () => {
    renderAt('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Pulse' })).toBeInTheDocument();
    const side = screen.getByRole('navigation', { name: 'Main' });
    expect(within(side).getByText('Admin')).toHaveClass('tz-side-tag');
    expect(within(side).getByText('Growth')).toBeInTheDocument();
    expect(within(side).getByText('Access')).toBeInTheDocument();
    expect(within(side).getAllByRole('link')).toHaveLength(NAV.length);
    expect(within(side).getByRole('link', { name: 'Pulse' })).toHaveAttribute('aria-current', 'page');
  });

  it('names every sidebar link and gives it a tooltip for the icon rail', async () => {
    renderAt('/');
    const side = await screen.findByRole('navigation', { name: 'Main' });
    for (const link of within(side).getAllByRole('link')) {
      const name = link.textContent;
      expect(name).toBeTruthy();
      expect(link).toHaveAttribute('title', name);
      expect(within(side).getByRole('link', { name: name ?? '' })).toBe(link);
    }
  });

  it.each(NAV.filter((n) => n.section !== ''))('shows the placeholder for /$section', async ({ section }) => {
    renderAt(`/${section}`);
    const side = await screen.findByRole('navigation', { name: 'Main' });
    const active = within(side).getAllByRole('link').find((l) => l.getAttribute('aria-current') === 'page');
    expect(active?.getAttribute('href')).toBe(`/${section}`);
    expect(screen.getByText('Coming soon')).toBeInTheDocument();
  });

  it('shows not found for unknown paths', async () => {
    renderAt('/no-such-page');
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to pulse' })).toHaveAttribute('href', '/');
  });

  it('renders the pseudo-locale with every key present', async () => {
    renderAt('/', asMessages(xa));
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.textContent).toMatch(/^\[.*\]$/);
  });
});
