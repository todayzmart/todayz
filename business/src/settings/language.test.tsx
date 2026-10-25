import en from '@todayz/i18n/compiled/business/en.json';
import hi from '@todayz/i18n/compiled/business/hi.json';
import { LANGUAGE_STORAGE_KEY, LOCALES, NATIVE_NAMES, asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { loadMessages } from '../app/i18n';
import { json, renderAt, type Call } from '../test/render';

/** Plain text of a compiled message with no arguments (the catalogues hold ICU ASTs). */
const text = (m: unknown) => (m as { value: string }[]).map((p) => p.value).join('');

function shops(c: Call) {
  return c.method === 'GET' && c.path === '/v1/biz/businesses'
    ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role: 'cashier', stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
    : undefined;
}

beforeEach(() => {
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
  window.localStorage.clear();
});
afterEach(() => vi.unstubAllGlobals());

describe('business catalogues', () => {
  it('every language has every English key, so nothing falls back to English', async () => {
    const keys = Object.keys(en).sort();
    for (const lc of LOCALES) {
      const messages = await loadMessages(lc);
      expect(Object.keys(messages).sort(), lc).toEqual(keys);
    }
  });

  it('the Indian-language catalogues are translated, not English copies', async () => {
    for (const lc of ['hi', 'kn', 'te', 'ta'] as const) {
      const messages = await loadMessages(lc);
      expect(messages['orders.title'], lc).not.toBe(en['orders.title']);
      expect(messages['stock.title'], lc).not.toBe(en['stock.title']);
    }
  });
});

describe('B-14 language', () => {
  it('any role can switch to Hindi; the page re-renders in Hindi and the choice is remembered', async () => {
    renderAt('/settings', { messages: asMessages(en), handlers: [shops] });
    const hindi = await screen.findByRole('button', { name: new RegExp(NATIVE_NAMES.hi) });
    expect(screen.getByRole('heading', { name: text(en['settings.language.title']) })).toBeInTheDocument();

    fireEvent.click(hindi);

    expect(await screen.findByRole('heading', { name: text(hi['settings.language.title']) })).toBeInTheDocument();
    expect(screen.getByText(text(hi['settings.noAccess.title']))).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: text(en['settings.language.title']) })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: new RegExp(NATIVE_NAMES.hi) })).toHaveAttribute('aria-pressed', 'true');
    await waitFor(() => expect(document.documentElement.lang).toBe('hi'));
    expect(window.localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe('hi');
  });
});
