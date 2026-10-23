import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { safeNext } from '../app/next';
import { renderAt } from './render';

describe('C-01 language', () => {
  it('first run on / goes to the language screen; Skip stores the choice and moves on', async () => {
    renderAt('/', { chosen: false });
    expect(await screen.findByRole('heading', { level: 1, name: 'Choose your language' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Skip' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Where should we look?' })).toBeInTheDocument();
    expect(localStorage.getItem('todayz.lang')).toBe('en');
  });

  it('a stored choice skips the screen', async () => {
    renderAt('/', { chosen: 'en' });
    expect(await screen.findByRole('heading', { level: 1, name: 'Everything nearby, today.' })).toBeInTheDocument();
  });

  it('choosing a language stores it and sets <html lang>', async () => {
    renderAt('/language');
    fireEvent.click(await screen.findByRole('button', { name: /हिन्दी/ }));
    await waitFor(() => expect(localStorage.getItem('todayz.lang')).toBe('hi'));
    await waitFor(() => expect(document.documentElement.lang).toBe('hi'));
  });
});

describe('C-02 location', () => {
  it('saves a valid PIN code and shows it on Home', async () => {
    renderAt('/location');
    fireEvent.change(await screen.findByLabelText('PIN code'), { target: { value: '560001' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use this PIN code' }));
    expect(await screen.findByRole('link', { name: /Shopping near 560001/ })).toBeInTheDocument();
  });

  it('refuses a PIN code that is not 6 digits', async () => {
    renderAt('/location');
    fireEvent.change(await screen.findByLabelText('PIN code'), { target: { value: '5600' } });
    fireEvent.click(screen.getByRole('button', { name: 'Use this PIN code' }));
    expect(await screen.findByText('Enter a 6-digit PIN code.')).toBeInTheDocument();
  });
});

describe('safeNext', () => {
  it('keeps same-app paths and drops everything else', () => {
    expect(safeNext('/orders/01J9Z?tab=1')).toBe('/orders/01J9Z?tab=1');
    for (const bad of ['//evil.example', 'https://evil.example', '/\\evil', 'orders', undefined, 7, '/sign-in?next=/x']) {
      expect(safeNext(bad)).toBe('/');
    }
  });
});
