import xa from '@todayz/i18n/compiled/admin/en-XA.json';
import { asMessages } from '@todayz/i18n';
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderAt } from '../test/render';
import { NAV, PLACEHOLDER_SECTIONS } from './nav';

describe('admin shell', () => {
  it('renders Pulse with the Admin-tagged sidebar', async () => {
    renderAt('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Pulse' })).toBeInTheDocument();
    const side = screen.getByRole('navigation', { name: 'Main' });
    expect(within(side).getByText('Admin')).toHaveClass('tz-side-tag');
    expect(within(side).getByText('Growth')).toBeInTheDocument();
    expect(within(side).getByText('Access')).toBeInTheDocument();
    expect(await within(side).findAllByRole('link')).toHaveLength(NAV.length);
    expect(within(side).getByRole('link', { name: 'Pulse' })).toHaveAttribute('aria-current', 'page');
  });

  it('names every sidebar link and gives it a tooltip for the icon rail', async () => {
    renderAt('/');
    const side = await screen.findByRole('navigation', { name: 'Main' });
    for (const link of await within(side).findAllByRole('link')) {
      const name = link.textContent;
      expect(name).toBeTruthy();
      expect(link).toHaveAttribute('title', name);
      expect(within(side).getByRole('link', { name: name ?? '' })).toBe(link);
    }
  });

  it.each(NAV.filter((n) => PLACEHOLDER_SECTIONS.includes(n.section)))('shows the placeholder for /$section', async ({ section }) => {
    renderAt(`/${section}`);
    const side = await screen.findByRole('navigation', { name: 'Main' });
    const active = (await within(side).findAllByRole('link')).find((l) => l.getAttribute('aria-current') === 'page');
    expect(active?.getAttribute('href')).toBe(`/${section}`);
    expect(await screen.findByText('Coming soon')).toBeInTheDocument();
  });

  it('shows not found for unknown paths', async () => {
    renderAt('/no-such-page');
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to pulse' })).toHaveAttribute('href', '/');
  });

  it('renders the pseudo-locale with every key present', async () => {
    renderAt('/', { messages: asMessages(xa) });
    const h1 = await screen.findByRole('heading', { level: 1 });
    expect(h1.textContent).toMatch(/^\[.*\]$/);
  });
});
