import en from '@todayz/i18n/compiled/business/en.json';
import { ApiError } from '@todayz/api-client';
import { createUpdateController } from '@todayz/auth';
import { asMessages } from '@todayz/i18n';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { IntlProvider } from 'react-intl';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UpdateContext } from '../app/update-context';
import { NARROW_QUERY } from '../shell/useNarrow';
import { memoryStorage } from '../sync/storage';
import type { PosApi, SaleAck, SyncRequest, SyncResponse } from './api';
import { clearOpenBills } from './openBill';
import { Counter } from './Counter';
import type { PosStoreRef } from './context';
import { PosProvider } from './runtime';
import type { CatalogueItem } from './types';

const SERVER_NOW = '2026-10-24T10:00:00.000Z';
const SETTINGS = { creditMinOrderValuePaise: null, reversalWindowHours: null, offlineLinkMaxHours: null };

const RICE: CatalogueItem = { id: 'item-rice', name: 'Rice 1 kg', packSize: 1, unit: 'kg', barcodes: ['8901234567890'], pricePaise: 5000, available: 10, isListed: true, topRank: 1 };
// Two of Sugar's three units are blocked for accepted online orders (D-270).
const SUGAR: CatalogueItem = { id: 'item-sugar', name: 'Sugar 1 kg', packSize: 1, unit: 'kg', barcodes: [], pricePaise: 4500, onHand: 3, reserved: 2, available: 1, isListed: true, topRank: 2 };
const SALT: CatalogueItem = { id: 'item-salt', name: 'Salt 1 kg', packSize: 1, unit: 'kg', barcodes: [], pricePaise: 2500, onHand: 0, reserved: 0, available: 0, isListed: true, topRank: 3 };

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
    getSnapshot: () => Promise.resolve({ items: [RICE, SUGAR, SALT], cursor: 'c0', settings: SETTINGS, serverNow: SERVER_NOW }),
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
  };
  return s;
}

interface Options {
  role?: PosStoreRef['role'];
  online?: boolean;
  narrow?: boolean;
  /** Render under StrictMode, which runs every effect twice as the dev build does. */
  strict?: boolean;
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
  await storage.saveCatalogue([RICE, SUGAR, SALT], { cursor: 'c0', settings: SETTINGS, syncedAt: SERVER_NOW });
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
  const app = <RouterProvider router={router} />;
  const view = render(opts.strict ? <StrictMode>{app}</StrictMode> : app);
  await screen.findAllByRole('button', { name: /Rice 1 kg/ });
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

afterEach(() => sessionStorage.clear());

describe('B-05 counter', () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('keeps the open bill when the counter is left and opened again, and not after sign-out clears it (D-279)', async () => {
    const first = await mount();
    await first.user.click(tile(/Rice 1 kg/));
    await first.user.click(tile(/Rice 1 kg/));
    first.view.unmount();
    const again = await mount();
    expect(within(screen.getByRole('complementary')).getByText('2 items')).toBeInTheDocument();
    expect(due()).toHaveTextContent('₹100');
    expect(sessionStorage.getItem('todayz.biz.pos.openBill.s1')).not.toBeNull();
    clearOpenBills();
    again.view.unmount();
    await mount();
    expect(screen.queryByText('2 items')).not.toBeInTheDocument();
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

  it('shows units blocked for online orders on the tile, and never a count below 0', async () => {
    await mount();
    expect(tile(/Sugar 1 kg/)).toHaveTextContent('1 in stock');
    expect(tile(/Sugar 1 kg/)).toHaveTextContent('2 blocked for online orders');
    expect(tile(/Rice 1 kg/)).not.toHaveTextContent('blocked');
  });

  it('asks for a count instead of raising a line past available; the count leaves out the blocked units by default', async () => {
    const { user, storage } = await mount();
    await user.click(tile(/Sugar 1 kg/));
    const bill = within(screen.getByRole('complementary'));
    await user.click(bill.getByRole('button', { name: /^Add one/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Update stock' });
    expect(within(dialog).getByText('Blocked for online orders').nextSibling).toHaveTextContent('2');
    const box = within(dialog).getByRole('checkbox', { name: 'This count includes the 2 units set aside for online orders' });
    expect(box).not.toBeChecked();
    // Still one in the bill while the dialog is open.
    expect(due()).toHaveTextContent('₹45');
    await user.type(within(dialog).getByLabelText('Counted now'), '4');
    expect(within(dialog).getByText(/On hand will be 6, available 4/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Save count' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Update stock' })).toBeNull());
    expect(due()).toHaveTextContent('₹90');
    const entries = await storage.listEntries();
    expect(entries.find((e) => e.kind === 'movement')?.payload).toEqual({ storeItemId: SUGAR.id, kind: 'count_correction', quantity: 6 });
    expect(tile(/Sugar 1 kg/)).toHaveTextContent('4 in stock');
  });

  it('takes the count as the whole shelf when the blocked units are included', async () => {
    const { user, storage } = await mount();
    await user.click(tile(/Sugar 1 kg/));
    await user.click(within(screen.getByRole('complementary')).getByRole('button', { name: /^Add one/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Update stock' });
    await user.click(within(dialog).getByRole('checkbox'));
    await user.type(within(dialog).getByLabelText('Counted now'), '2');
    await user.click(within(dialog).getByRole('button', { name: 'Save count' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Update stock' })).toBeNull());
    // On hand 2, both blocked for online orders: nothing is left to sell, so the line goes.
    expect((await storage.listEntries()).find((e) => e.kind === 'movement')?.payload).toMatchObject({ quantity: 2 });
    expect(within(screen.getByRole('complementary')).queryByText('Sugar 1 kg')).toBeNull();
  });

  it('opens the dialog with no checkbox when nothing is blocked, and Cancel leaves the bill as it was', async () => {
    const { user, storage } = await mount();
    await user.click(tile(/Salt 1 kg/));
    const dialog = await screen.findByRole('dialog', { name: 'Update stock' });
    expect(within(dialog).queryByRole('checkbox')).toBeNull();
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Update stock' })).toBeNull());
    expect(within(screen.getByRole('complementary')).queryByText('Salt 1 kg')).toBeNull();
    expect(await storage.listEntries()).toHaveLength(0);
  });

  it('opens the dialog for the item when the server finds an online sale short, and keeps the sale', async () => {
    const { user, server, storage } = await mount();
    server.failSale = new ApiError({ type: 'about:blank', title: 'x', status: 409, code: 'insufficient_stock', extensions: { storeItemId: RICE.id, available: 0 } }, new Headers());
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    const dialog = await screen.findByRole('dialog', { name: 'Update stock' });
    expect(within(dialog).getByText(/This sale went through/)).toBeInTheDocument();
    expect((await storage.listEntries())[0]).toMatchObject({ kind: 'sale', state: 'pending', lastError: 'insufficient_stock' });
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

  it('Sync now shows a spinner while the device sends its sales', async () => {
    const { user, server } = await mount({ online: false });
    await user.click(tile(/Rice 1 kg/));
    await user.click(charge());
    await screen.findByText(/saved on this device/);
    // The connection comes back and the send is held open.
    server.api.sync = () => new Promise(() => {});
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);
    act(() => void window.dispatchEvent(new Event('online')));
    await user.click(screen.getAllByRole('button', { name: /^Sales/ })[0] as HTMLElement);
    const sales = await screen.findByRole('dialog', { name: 'Sales on this device' });
    await waitFor(() => expect(within(sales).getByRole('button', { name: 'Sync now' })).toHaveAttribute('aria-busy', 'true'));
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

  it('points to Orders → Counter sales for every device\'s sales (D-268)', async () => {
    await mount({ role: 'cashier' });
    const link = screen.getByRole('link', { name: 'View sales' });
    expect(link.getAttribute('href')).toBe('/orders?view=sales');
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

  it('makes the bill QR on the first try when the effect runs twice', async () => {
    const { user, server } = await mount({ strict: true });
    // As the server does: the same Idempotency-Key while its request is in flight is a 409.
    const inFlight = new Set<string>();
    let calls = 0;
    server.api.createBillClaim = (_draft, key) => {
      calls++;
      if (inFlight.has(key)) return Promise.reject(new Error('409 conflict'));
      inFlight.add(key);
      return new Promise((resolve) =>
        setTimeout(() => {
          inFlight.delete(key);
          resolve({ claimId: 'claim-1', token: 'EXAMPLE_token', expiresAt: '2026-10-24T10:02:00.000Z', serverNow: SERVER_NOW });
        }, 20),
      );
    };
    await user.click(tile(/Rice 1 kg/));
    await user.click(screen.getByRole('button', { name: 'Link customer' }));
    const dialog = await screen.findByRole('dialog', { name: 'Link a customer' });
    await user.click(within(dialog).getByRole('button', { name: /Customer scans a QR/ }));
    expect(await within(dialog).findByRole('img', { name: /QR/ })).toBeInTheDocument();
    expect(within(dialog).queryByText("Couldn't make the QR.")).toBeNull();
    expect(calls).toBe(1);
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
