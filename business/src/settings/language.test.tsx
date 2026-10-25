import en from '@todayz/i18n/compiled/business/en.json';
import hi from '@todayz/i18n/compiled/business/hi.json';
import { LANGUAGE_STORAGE_KEY, LOCALES, NATIVE_NAMES, asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { loadMessages } from '../app/i18n';
import { json, renderAt, type Call } from '../test/render';

/** The source catalogues: compiled ones fill a missing translation with English, so they cannot show a fallback. */
const messagesDir = dirname(createRequire(import.meta.url).resolve('@todayz/i18n/messages/business/pos.en.json'));
const readArea = (file: string) => JSON.parse(readFileSync(join(messagesDir, file), 'utf8')) as Record<string, Record<string, string>>;
/** Same hash as shared/i18n/scripts/catalogue.mjs: a translation of older English is stale and ships as English. */
const enHash = (msg: string) => createHash('sha1').update(msg).digest('hex').slice(0, 8);

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
  it('every English key has a reviewed, current translation in each Indian language, so nothing falls back to English', () => {
    const missing: string[] = [];
    for (const file of readdirSync(messagesDir).filter((f) => f.endsWith('.en.json'))) {
      const english = readArea(file);
      for (const lc of ['hi', 'kn', 'te', 'ta']) {
        const translated = readArea(file.replace('.en.json', `.${lc}.json`));
        for (const [key, { defaultMessage }] of Object.entries(english)) {
          const t = translated[key];
          if (t?.review !== 'reviewed' || t.enHash !== enHash(defaultMessage ?? '')) missing.push(`${lc} ${key}`);
        }
      }
    }
    expect(missing).toEqual([]);
  });

  it('every compiled catalogue has every English key', async () => {
    const keys = Object.keys(en).sort();
    for (const lc of LOCALES) expect(Object.keys(await loadMessages(lc)).sort(), lc).toEqual(keys);
  });

  it('the Indian-language catalogues are translated, not English copies', async () => {
    for (const lc of ['hi', 'kn', 'te', 'ta'] as const) {
      const messages = await loadMessages(lc);
      expect(messages['orders.title'], lc).not.toEqual(en['orders.title']);
      expect(messages['stock.title'], lc).not.toEqual(en['stock.title']);
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
