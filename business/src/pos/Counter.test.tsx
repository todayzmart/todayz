import en from '@todayz/i18n/compiled/business/en.json';
import { createUpdateController } from '@todayz/auth';
import { asMessages } from '@todayz/i18n';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { IntlProvider } from 'react-intl';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UpdateContext } from '../app/update-context';
import { NARROW_QUERY } from '../shell/useNarrow';
import { memoryStorage } from '../sync/storage';
import type { PosApi, SaleAck, StoreSale, SyncRequest, SyncResponse } from './api';
import { Counter } from './Counter';
import type { PosStoreRef } from './context';
import { PosProvider } from './runtime';
import type { CatalogueItem } from './types';

const SERVER_NOW = '2026-10-24T10:00:00.000Z';
const SETTINGS = { creditMinOrderValuePaise: null, reversalWindowHours: null, offlineLinkMaxHours: null };

const RICE: CatalogueItem = { id: 'item-rice', name: 'Rice 1 kg', packSize: 1, unit: 'kg', barcodes: ['8901234567890'], pricePaise: 5000, available: 10, isListed: true, topRank: 1 };
const SUGAR: CatalogueItem = { id: 'item-sugar', name: 'Sugar 1 kg', packSize: 1, unit: 'kg', barcodes: [], pricePaise: 4500, available: 1, isListed: true, topRank: 2 };

interface Server {
  api: PosApi;
  created: { clientId: string }[];
  syncs: SyncRequest[];
  /** What createSale and sync answer for a sale; `refuse` makes the next sync reject it. */
  refuse: string | null;
  failSale: unknown;
  reversed: { saleId: string; reason: string }[];
}

function fakeServer(): Server {
  const s: Server = { created: [], syncs: [], refuse: null, failSale: null, reversed: [], api: undefined as unknown as PosApi };
  const ack = (clientId: string, totalPaise = 5000): SaleAck => ({ saleId: `sale-${clientId}`, receiptToken: `tok-${clientId}`, roundOffPaise: 0, totalPaise, serverNow: SERVER_NOW });
  s.api = {
    getSnapshot: () => Promise.resolve({ items: [RICE, SUGAR], cursor: 'c0', settings: SETTINGS, serverNow: SERVER_NOW }),
    getChanges: () => Promise.resolve({ changes: [], cursor: 'c1', settings: SETTINGS, serverNow: SERVER_NOW }),
    getItem: () => Promise.reject(new Error('unused')),
    sync: (body) => {
      s.syncs.push(structuredClone(body));
      const results: SyncResponse['results'] = body.entries.map((e) => {
        if (s.refuse) return { clientId: e.clientId, status: 'rejected' as const, code: s.refuse };
        return { clientId: e.clientId, status: 'applied' as const, ...ack(e.clientId, (e.payload as { totalPaise?: number }).totalPaise ?? 5000) };
      });
      return Promise.resolve({ results, changes: [], cursor: 'c2', serverNow: SERVER_NOW });
    },
    createSale: (wire) => {
      if (s.failSale) return Promise.reject(s.failSale);
      s.created.push({ clientId: wire.clientId });
      return Promise.resolve(ack(wire.clientId, wire.lines.reduce((n, l) => n + l.unitPricePaise * l.quantity - (l.manualDiscountPaise ?? 0), 0) - (wire.billDiscountPaise ?? 0)));
    },
    reverseSale: (saleId, _key, reason) => {
      s.reversed.push({ saleId, reason });
      return Promise.resolve({ stockReturned: [{ storeItemId: RICE.id, quantity: 1 }], creditsReturned: 0, cashbackClawedBack: 0 });
    },
    createBillClaim: () => Promise.reject(new Error('unused')),
    getBillClaim: () => Promise.reject(new Error('unused')),
    verifyCounterCode: () => Promise.reject(new Error('unused')),
    listSales: () => Promise.resolve({ items: [], nextCursor: null }),
  };
  return s;
}

interface Options {
  role?: PosStoreRef['role'];
  online?: boolean;
  narrow?: boolean;
}

async function mount(opts: Options = {}) {
  const online = opts.online ?? true;
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(online);
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === NARROW_QUERY ? (opts.narrow ?? false) : false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }));
  const server = fakeServer();
  const storage = memoryStorage();
  // A device that has already downloaded its catalogue, so the counter works offline from the first paint.
  await storage.saveCatalogue([RICE, SUGAR], { cursor: 'c0', settings: SETTINGS, syncedAt: SERVER_NOW });
  const store: PosStoreRef = { bid: 'b1', sid: 's1', name: 'Asha Stores', role: opts.role ?? 'owner' };
  const user = userShim();
  const tree = (
    <IntlProvider locale="en-IN" defaultLocale="en-IN" messages={asMessages(en)}>
      <UpdateContext.Provider value={createUpdateController()}>
        <PosProvider store={store} api={server.api} storage={storage} now={() => Date.parse(SERVER_NOW)}>
          <Counter />
        </PosProvider>
      </UpdateContext.Provider>
    </IntlProvider>
  );
  // The add-product sheet links to Stock, so the counter needs a router around it.
  const router = createRouter({ routeTree: createRootRoute({ component: () => tree }), history: createMemoryHistory({ initialEntries: ['/'] }) });
  const view = render(<RouterProvider router={router} />);
  await screen.findByRole('button', { name: /Rice 1 kg/ });
  return { server, storage, user, view };
}

/** No user-event package here, so a click and typing are the two DOM events the app listens for. */
function userShim() {
  return {
    click: (el: Element) => {
      act(() => void fireEvent.click(el));
      return Promise.resolve();
    },
    type: (el: Element, text: string) => {
      const [value, ...rest] = text.split('{Enter}');
      if (value) act(() => void fireEvent.change(el, { target: { value: (el as HTMLInputElement).value + value } }));
      if (rest.length > 0) act(() => void fireEvent.keyDown(el, { key: 'Enter' }));
      return Promise.resolve();
    },
  };
}

const tile = (name: RegExp) => within(screen.getByRole('region', { name: 'Items' })).getByRole('button', { name });
/** The amount to take sits beside the Charge button, in the bill's live total. */
const due = () => within(screen.getByRole('complementary')).getByText(/^₹[\d,.]+$/, { selector: '.tz-amt' });
const charge = () => screen.getByRole('button', { name: 'Charge' });

describe('B-05 counter', () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows the quick tiles with stock, and adds an item to the bill with a tap', async () => {
    const { user } = await mount();
    expect(tile(/Rice 1 kg/)).toHaveTextContent('10 in stock');
    await user.click(tile(/Rice 1 kg/));
    await user.click(tile(/Rice 1 kg/));
    // Two of the same item merge into one line of 2.
    const cart = screen.getByRole('complementary');
    expect(within(cart).getAllByText('Rice 1 kg')).toHaveLength(1);
    expect(within(cart).getByText('2 items')).toBeInTheDocument();
    expect(due()).toHaveTextContent('₹100');
  });

  it('searches by name and adds the first hit on Enter', async () => {
    const { user } = await mount();
    await user.type(screen.getByLabelText('Search or scan an item'), 'sug{Enter}');
    const cart = screen.getByRole('complementary');
    expect(within(cart).getByText('Sugar 1 kg')).toBeInTheDocument();
    expect(screen.getByLabelText('Search or scan an item')).toHaveValue('');
  });

  it('adds by barcode, and opens the add sheet with the code when it is unknown', async () => {
    const { user } = await mount();
    const box = screen.getByLabelText('Search or scan an item');
    await user.type(box, '8901234567890{Enter}');
    expect(within(screen.getByRole('complementary')).getByText('Rice 1 kg')).toBeInTheDocument();
    await user.type(box, '9990001112223{Enter}');
    const dialog = await screen.findByRole('dialog', { name: "We don't know this barcode" });
    expect(dialog).toHaveTextContent('9990001112223');
    await user.type(within(dialog).getByLabelText('Item name'), 'Loose sugar');
    await user.type(within(dialog).getByLabelText('Price'), '40');
    await user.click(within(dialog).getByRole('button', { name: 'Add to bill' }));
    expect(within(screen.getByRole('complementary')).getByText('Loose sugar')).toBeInTheDocument();
  });

  it('flags a line that is more than the count on hand but still lets the sale through', async () => {
    const { user } = await mount();
    await user.click(tile(/Sugar 1 kg/));
    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: /^Add one/ }));
    expect(await screen.findByText(/more than the count on hand/)).toBeInTheDocument();
    expect(charge()).toBeEnabled();
  });

  it('charges online: records on the device, sends it once, shows the sale as done', async () => {
    const { user, server, storage } = await mount();
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'UPI' }));
    await user.click(charge());
    expect(await screen.findByText('Sale of ₹50 done')).toBeInTheDocument();
    expect(server.created).toHaveLength(1);
    // The bill is clear and a new one starts.
    expect(within(screen.getByRole('complementary')).queryByText('Rice 1 kg')).toBeNull();
    // The entry was recorded with a device sequence and has left the outbox once acknowledged.
    await waitFor(async () => expect((await storage.listEntries()).filter((e) => e.state === 'pending')).toHaveLength(0));
  });

  it('charges offline: the sale is safe on the device and waits, with the offline banner shown', async () => {
    const { user, server, storage } = await mount({ online: false });
    expect(screen.getByText(/^Offline/)).toBeInTheDocument();
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    expect(await screen.findByText(/Sale of ₹50 saved on this device/)).toBeInTheDocument();
    expect(server.created).toHaveLength(0);
    expect(server.syncs).toHaveLength(0);
    const entries = await storage.listEntries();
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ state: 'pending', kind: 'sale', seq: 1 });
    expect(screen.getByText(/Offline · 1 sale waiting to sync/)).toBeInTheDocument();
  });

  it('keeps the bill and says so when the sale cannot be saved', async () => {
    const { user, storage } = await mount();
    vi.spyOn(storage, 'putEntry').mockRejectedValue(new Error('quota'));
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    expect(await screen.findByText(/couldn't save|could not save|not saved/i)).toBeInTheDocument();
    expect(within(screen.getByRole('complementary')).getByText('Rice 1 kg')).toBeInTheDocument();
  });

  it('F4 charges and F2 focuses the search', async () => {
    const { user } = await mount();
    await user.click(tile(/Rice 1 kg/));
    fireEvent.keyDown(document, { key: 'F2' });
    expect(screen.getByLabelText('Search or scan an item')).toHaveFocus();
    fireEvent.keyDown(document, { key: 'F4' });
    expect(await screen.findByText('Sale of ₹50 done')).toBeInTheDocument();
  });

  it('lets a manager discount the bill and shows the total as the server returns it', async () => {
    const { user, server } = await mount({ role: 'manager' });
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'Discount' }));
    const dialog = await screen.findByRole('dialog');
    const bill = within(dialog).getByLabelText('Whole bill discount');
    await user.type(bill, '5');
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(due()).toHaveTextContent('₹45'));
    await user.click(charge());
    expect(await screen.findByText('Sale of ₹45 done')).toBeInTheDocument();
    expect(server.created).toHaveLength(1);
  });

  it('hides Discount from a cashier', async () => {
    const { user } = await mount({ role: 'cashier' });
    await user.click(tile(/Rice 1 kg/));
    expect(screen.queryByRole('button', { name: 'Discount' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Link customer' })).toBeEnabled();
  });

  it('notes an unverified phone against the sale and sends it with the sale', async () => {
    const { user, server } = await mount({ online: false });
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'Link customer' }));
    const dialog = await screen.findByRole('dialog', { name: 'Link a customer' });
    await user.click(within(dialog).getByRole('button', { name: /Just note their phone/ }));
    await user.type(within(dialog).getByLabelText('Customer phone'), '9000000001');
    await user.click(within(dialog).getByRole('button', { name: 'Note this phone' }));
    await waitFor(() => expect(screen.getAllByText(/•••• 0001/).length).toBeGreaterThan(0));
    await user.click(charge());
    await screen.findByText(/saved on this device/);
    expect(server.syncs).toHaveLength(0);
  });

  it('empties the bill with Clear bill', async () => {
    const { user } = await mount();
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'Clear bill' }));
    expect(within(screen.getByRole('complementary')).queryByText('Rice 1 kg')).toBeNull();
    expect(charge()).toBeDisabled();
  });

  it('shows a refused sale with a way to review it, and the sales list says why', async () => {
    const { user, server } = await mount({ online: false });
    server.refuse = 'claim_expired';
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    await screen.findByText(/saved on this device/);
    // The network comes back and the sale is delivered, then refused by the server.
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);
    act(() => void window.dispatchEvent(new Event('online')));
    expect(await screen.findByText(/1 sale needs your attention/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Review' }));
    const sales = await screen.findByRole('dialog', { name: 'Sales on this device' });
    expect(within(sales).getByText('Refused')).toBeInTheDocument();
    expect(within(sales).getByText(/server refused this sale/)).toBeInTheDocument();
  });

  it('opens the receipt of the sale just rung', async () => {
    const { user } = await mount();
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    await screen.findByText('Sale of ₹50 done');
    await user.click(screen.getByRole('button', { name: 'Receipt' }));
    const receipt = await screen.findByRole('dialog', { name: 'Receipt' });
    expect(within(receipt).getByText('Asha Stores')).toBeInTheDocument();
    expect(within(receipt).getByText('Rice 1 kg')).toBeInTheDocument();
  });

  it('lets a manager reverse a synced sale from the sales list, with a reason, and not a cashier', async () => {
    const m = await mount({ role: 'manager' });
    await m.user.click(tile(/Rice 1 kg/));
    await m.user.click(charge());
    await screen.findByText('Sale of ₹50 done');
    await m.user.click(screen.getByRole('button', { name: /^Sales/ }));
    const sales = await screen.findByRole('dialog', { name: 'Sales on this device' });
    await m.user.click(await within(sales).findByRole('button', { name: 'Reverse' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Reverse this sale?' });
    await m.user.type(within(confirm).getByLabelText('Reason'), 'Wrong item');
    await m.user.click(within(confirm).getByRole('button', { name: 'Reverse sale' }));
    await waitFor(() => expect(m.server.reversed).toEqual([{ saleId: expect.stringContaining('sale-'), reason: 'Wrong item' }]));
    m.view.unmount();

    const c = await mount({ role: 'cashier' });
    await c.user.click(tile(/Rice 1 kg/));
    await c.user.click(charge());
    await screen.findByText('Sale of ₹50 done');
    await c.user.click(screen.getByRole('button', { name: /^Sales/ }));
    const list = await screen.findByRole('dialog', { name: 'Sales on this device' });
    await within(list).findByText('Synced');
    expect(within(list).queryByRole('button', { name: 'Reverse' })).toBeNull();
  });

  it('lists the store sales from the server with each link status, the failure reason and more pages', async () => {
    const { user, server } = await mount({ role: 'cashier' });
    const base = { totalPaise: 30000, unitCount: 3, paymentMethod: 'cash' as const, reversed: false, offline: true };
    const page1: StoreSale[] = [
      { ...base, id: 's-1', soldAt: '2026-10-24T09:30:00.000Z', link: { status: 'failed', customerName: null, failure: 'code_used' } },
      { ...base, id: 's-2', soldAt: '2026-10-24T09:00:00.000Z', link: { status: 'linked', customerName: 'Ananya', failure: null } },
    ];
    const page2: StoreSale[] = [
      { ...base, id: 's-3', soldAt: '2026-10-24T08:00:00.000Z', link: { status: 'failed', customerName: null, failure: 'clock_off' } },
      { ...base, id: 's-4', soldAt: '2026-10-24T07:00:00.000Z', offline: false, paymentMethod: 'upi', unitCount: 1, totalPaise: 5000, link: null },
    ];
    const calls: (string | null)[] = [];
    server.api.listSales = (cursor) => {
      calls.push(cursor);
      return Promise.resolve(cursor === 'next-1' ? { items: page2, nextCursor: null } : { items: page1, nextCursor: 'next-1' });
    };
    await user.click(screen.getByRole('button', { name: 'Store sales' }));
    const sheet = await screen.findByRole('dialog', { name: 'Store sales' });
    expect(await within(sheet).findByText('The counter code was already used.')).toBeInTheDocument();
    expect(within(sheet).getByText('Linked to Ananya')).toBeInTheDocument();
    expect(within(sheet).getAllByText('Customer link failed')).toHaveLength(1);
    expect(within(sheet).getAllByText(/3 units · Cash/)).toHaveLength(2);
    await user.click(within(sheet).getByRole('button', { name: 'Load more' }));
    expect(await within(sheet).findByText('The device clock was off.')).toBeInTheDocument();
    expect(within(sheet).getAllByText('Customer link failed')).toHaveLength(2);
    expect(within(sheet).getByText('No customer')).toBeInTheDocument();
    expect(within(sheet).getByText(/1 unit · UPI/)).toBeInTheDocument();
    expect(within(sheet).queryByRole('button', { name: 'Load more' })).toBeNull();
    expect(calls).toEqual([null, 'next-1']);
  });

  it('says the store sales need a connection when offline, and does not call the server', async () => {
    const { user, server } = await mount({ online: false });
    const listSales = vi.fn(server.api.listSales);
    server.api.listSales = listSales;
    await user.click(screen.getByRole('button', { name: 'Store sales' }));
    const sheet = await screen.findByRole('dialog', { name: 'Store sales' });
    expect(within(sheet).getByText(/You are offline/)).toBeInTheDocument();
    expect(within(sheet).getByRole('button', { name: 'Refresh' })).toBeDisabled();
    expect(listSales).not.toHaveBeenCalled();
  });

  it('drops a bill QR link when the bill changes after the claim, and says so', async () => {
    const { user, server } = await mount();
    const keys: string[] = [];
    server.api.createBillClaim = (_draft, key) => {
      keys.push(key);
      return Promise.resolve({ claimId: 'claim-1', token: 'EXAMPLE_token', expiresAt: '2026-10-24T10:02:00.000Z', serverNow: SERVER_NOW });
    };
    server.api.getBillClaim = () => Promise.resolve({ claimId: 'claim-1', status: 'claimed', expiresAt: '2026-10-24T10:02:00.000Z', claimedBy: { displayName: 'Asha' }, creditsRequested: 0, serverNow: SERVER_NOW });
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'Link customer' }));
    const dialog = await screen.findByRole('dialog', { name: 'Link a customer' });
    await user.click(within(dialog).getByRole('button', { name: /Customer scans a QR/ }));
    await waitFor(() => expect(screen.getAllByText(/Linked by bill QR/).length).toBeGreaterThan(0), { timeout: 4000 });
    // The contract's Idempotency-Key is a ULID; anything else is a 422 before the handler.
    expect(keys.length).toBeGreaterThan(0);
    for (const k of keys) expect(k).toMatch(/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Link a customer' })).toBeNull());
    // A second item changes the amount the QR was quoted for.
    await user.click(tile(/Sugar 1 kg/));
    expect(await screen.findByText('Customer link removed')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Link customer' })).toBeEnabled();
  });

  it('on a phone the bill sits behind a bar and a sheet', async () => {
    const { user } = await mount({ narrow: true });
    expect(screen.queryByRole('complementary')).toBeNull();
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: /View bill|View/ }));
    const sheet = await screen.findByRole('dialog');
    await user.click(within(sheet).getByRole('button', { name: 'Charge' }));
    expect(await screen.findByText('Sale of ₹50 done')).toBeInTheDocument();
  });
});
