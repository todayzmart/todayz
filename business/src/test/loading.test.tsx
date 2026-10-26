import en from '@todayz/i18n/compiled/business/en.json';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { failThenHold } from './held';
import { renderAt, type Call } from './render';

// The founder's ask (26 Oct): no API wait may leave the screen blank or a button dead. These cover
// the "Try again" buttons. A query that never loaded goes back to pending on retry, so the screen
// shows its skeleton; one that had data keeps its error and the button shows the spinner. Either
// way the main area is busy, never blank.

const msg = en as Record<string, unknown>;
const text = (id: string) => {
  const m = msg[id];
  // Compiled messages are an AST; the retry labels are plain literals.
  return Array.isArray(m) ? m.map((p: { value?: string }) => p.value ?? '').join('') : String(m);
};

beforeEach(() => {
  vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
});
afterEach(() => vi.unstubAllGlobals());

/** A skeleton or a busy control inside the main area. */
function busyMain(): boolean {
  const main = document.getElementById('main');
  return main !== null && main.querySelector('.tz-ui-skeleton, [aria-busy="true"]') !== null;
}

const businesses = (c: Call) => c.method === 'GET' && c.path === '/v1/biz/businesses';

// Every screen gated on the current store or business (useCurrentStore / useCurrentBusiness).
const GATED: [path: string, retryKey: string][] = [
  ['/', 'overview.retry'],
  ['/analytics', 'overview.retry'],
  ['/customers', 'overview.retry'],
  ['/orders', 'orders.error.retry'],
  ['/orders/o1', 'orders.error.retry'],
  ['/sell', 'pos.error.retry'],
  ['/stock', 'stock.error.retry'],
  ['/offers', 'offers.error.retry'],
  ['/reviews', 'reviews.error.retry'],
  ['/credits', 'credits.retry'],
  ['/standee', 'standee.retry'],
  ['/voice', 'voice.retry'],
  ['/help', 'help.error.retry'],
  ['/help/new', 'help.error.retry'],
  ['/help/t1', 'help.error.retry'],
  ['/settings', 'settings.retry'],
  ['/stores', 'stores.retry'],
  ['/onboarding', 'onboarding.retry'],
];

describe('store gate "Try again" shows a loading state while the retry runs', () => {
  it.each(GATED)('%s', async (path, key) => {
    const list = failThenHold(businesses);
    renderAt(path, { handlers: [list] });
    const buttons = await screen.findAllByRole('button', { name: text(key) });
    const button = buttons[buttons.length - 1] as HTMLElement;
    expect(button).not.toHaveAttribute('aria-busy');
    fireEvent.click(button);
    await waitFor(() => expect(list.calls()).toBeGreaterThan(1));
    await waitFor(() => expect(busyMain()).toBe(true));
  });
});

describe('B-01 shop picker', () => {
  it('"Try again" shows a spinner while the list is read again', async () => {
    const list = failThenHold(businesses);
    renderAt('/choose-shop', { handlers: [list] });
    const retry = await screen.findByRole('button', { name: text('shop.pick.retry') });
    fireEvent.click(retry);
    await waitFor(() => expect(busyMain()).toBe(true));
  });
});
