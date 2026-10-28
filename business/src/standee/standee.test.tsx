import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { holding } from '../test/held';
import { storeLink } from './link';

const messages = asMessages(en);
const B = '/v1/biz/businesses/b1';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
      : undefined;
}
const store: Handler = (c) => (c.method === 'GET' && c.path === `${B}/stores/s1` ? json(200, { id: 's1', shortCode: 'K7Q2X9' }) : undefined);

// The campaigns card (W3-09) asks for the standees too; an empty list keeps these tests on the store sheet.
const standees: Handler = (c) => (c.path === `${B}/stores/s1/standees` ? json(200, { items: [], totals: { scans: 0, signUps: 0, qualified: 0 }, limit: 20 }) : undefined);
const counts = { scans: 3, signUps: 1, qualified: 0 };
const gate: Handler = (c) =>
  c.path === `${B}/stores/s1/standees`
    ? json(200, {
        items: [{ id: 'sd1', storeId: 's1', campaign: 'Diwali gate', link: 'https://api.todayz.localhost/v1/public/ref/K7Q2X9?s=sd1', counts, lastScanAt: null, printedAt: null, reorderRequestedAt: null, createdAt: '2026-10-01T00:00:00Z', version: 1 }],
        totals: counts,
        limit: 20,
      })
    : undefined;

afterEach(() => vi.restoreAllMocks());

/** jsdom has no print dialog; the page only needs `window.print` to be called. */
function stubPrint() {
  const print = vi.fn();
  Object.defineProperty(window, 'print', { value: print, configurable: true });
  return print;
}

describe('standee page (D-278: drawn and printed in the browser)', () => {
  it('a cashier gets the no-access state', async () => {
    renderAt('/standee', { messages, handlers: [shops('cashier'), store] });
    expect(await screen.findByText('The standee is for owners and managers')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Print or save as PDF' })).not.toBeInTheDocument();
  });

  it('previews the A5 sheet with the shop name, the store QR and the short code, with no PDF request', async () => {
    const { calls } = renderAt('/standee', { messages, handlers: [shops('manager'), store, standees] });
    const preview = await screen.findByRole('figure', { name: 'Standee preview' });
    expect(within(preview).getByText('Indiranagar')).toBeInTheDocument();
    expect(within(preview).getByText('K7Q2X9')).toBeInTheDocument();
    expect(within(preview).getByRole('img', { name: 'QR code for Indiranagar' })).toBeInTheDocument();
    expect(storeLink('K7Q2X9')).toBe('https://api.todayz.localhost/v1/public/ref/K7Q2X9?k=s');
    expect(calls.some((c) => c.path.endsWith('.pdf'))).toBe(false);
  });

  it('"Print or save as PDF" opens the browser print dialog with only the sheet and ticks the setup step (D-281)', async () => {
    const print = stubPrint();
    const printed: Handler = (c) => (c.method === 'POST' && c.path === `${B}/stores/s1/standee-sheet/printed` ? new Response(null, { status: 204 }) : undefined);
    const { calls } = renderAt('/standee', { messages, handlers: [shops('owner'), store, standees, printed] });
    fireEvent.click(await screen.findByRole('button', { name: 'Print or save as PDF' }));
    await waitFor(() => expect(print).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === `${B}/stores/s1/standee-sheet/printed`)).toBe(true));
    const sheet = document.body.querySelector('[data-standee-print]');
    expect(sheet).not.toBeNull();
    expect(sheet?.textContent).toContain('K7Q2X9');
    fireEvent(window, new Event('afterprint'));
    await waitFor(() => expect(document.body.querySelector('[data-standee-print]')).toBeNull());
  });

  it('prints a campaign standee with its own link and its name at the foot', async () => {
    const print = stubPrint();
    renderAt('/standee', { messages, handlers: [shops('owner'), store, gate] });
    fireEvent.click(await screen.findByRole('button', { name: 'Print or save as PDF: Diwali gate' }));
    await waitFor(() => expect(print).toHaveBeenCalledTimes(1));
    expect(document.body.querySelector('[data-standee-print]')?.textContent).toContain('Diwali gate');
  });
});

describe('standee loading states', () => {
  it('shows a skeleton for the short code while the store loads, and no print button yet', async () => {
    renderAt('/standee', { messages, handlers: [shops('owner'), standees, holding((c) => c.method === 'GET' && c.path === `${B}/stores/s1`)] });
    await screen.findByRole('heading', { name: 'Print your standee' });
    await waitFor(() => expect(document.querySelector('.tz-ui-skeleton[style*="10ch"]')).not.toBeNull());
    expect(screen.queryByRole('button', { name: 'Print or save as PDF' })).not.toBeInTheDocument();
  });

  it('says so, with a retry, when the short code cannot be loaded', async () => {
    renderAt('/standee', { messages, handlers: [shops('owner'), standees, (c) => (c.method === 'GET' && c.path === `${B}/stores/s1` ? problem(503, 'service_unavailable') : undefined)] });
    // One retry (the app's query default), then the banner.
    expect(await screen.findByText('Could not load your short code.', undefined, { timeout: 4000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });
});
