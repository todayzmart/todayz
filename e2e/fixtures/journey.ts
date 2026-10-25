import { expect, request as playwrightRequest, test as base, type Browser, type BrowserContext, type Page } from '@playwright/test';

import { anon, totp, ulid, type Api } from './api';
import { principal, readState, setClockOffset, type Principal } from './principals';
import { LAKSHMI, PEOPLE, QA_POINT, URLS, type SeedStore } from './seed';

// The steps the journeys share. Every helper asserts through Api.ok, whose messages carry the step,
// the route and the problem code only: an order's pickup code, a bill token or a counter code is
// read from the API, used, and never printed or attached.

export interface Clock {
  now: string;
  offsetMs: number;
  dueJobs?: number;
}

/** Moves the stack's test clock (D-80), waiting up to `wait` seconds for due jobs, and records the offset. */
export async function moveClock(body: { advance: string } | { reset: true }, wait = 20): Promise<Clock> {
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  try {
    const r = await anon<Clock>(ctx, 'POST', '/v1/test/clock', { ...body, wait: `${wait}s` });
    expect(r.status, 'the test clock moved').toBe(200);
    setClockOffset(r.body.offsetMs);
    return r.body;
  } finally {
    await ctx.dispose();
  }
}

/** The server's now (the test clock), in ms. */
export async function serverNow(): Promise<number> {
  const ctx = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  try {
    const r = await anon<Clock>(ctx, 'GET', '/v1/test/clock');
    expect(r.status).toBe(200);
    setClockOffset(r.body.offsetMs);
    return Date.parse(r.body.now);
  } finally {
    await ctx.dispose();
  }
}

export interface Line {
  id: string;
  storeItemId: string;
  quantityRequested: number;
  quantityAccepted: number | null;
  unitPricePaise: number;
}

export interface Order {
  id: string;
  number: string;
  status: string;
  statusReason: string | null;
  fulfilment: string;
  lines: Line[];
  totalPaise: number;
  creditsUsed: number;
  cashbackCredits: number;
  code: string | null;
  holdUntil: string | null;
  itemsAfterOffersPaise: number;
  deliveryFeePaise: number;
}

/** An order as the shop reads it: the customer's full phone only while out for delivery (BK-27). Never print it. */
export interface StoreOrder extends Order {
  customer: { name: string | null; phoneMasked: string | null; phone: string | null };
}

export interface Address {
  addressId: string;
  label: string;
  isDefault: boolean;
}

export interface Item {
  storeItemId: string;
  quantity: number;
}

export interface Quote {
  totalPaise: number;
  creditsAllowed: number;
  creditsUsed: number;
  creditsLimit: unknown;
  itemsAfterOffersPaise: number;
  orderable: boolean;
}

export interface Wallet {
  availableCredits: number;
  heldCredits: number;
}

export interface CreditAccount {
  balanceCredits: number;
  stores: { storeId: string; balanceCredits: number }[];
  lines: { id: string; storeId: string; kind: string; credits: number; orderNumber: string | null }[];
}

/** A shop, as one staff member sees it through the business API. */
export class Shop {
  readonly base: string;
  constructor(
    readonly staff: Principal,
    readonly store: SeedStore,
    readonly businessId: string = LAKSHMI.id,
  ) {
    this.base = `/v1/biz/businesses/${businessId}/stores/${store.id}`;
  }

  get api(): Api {
    return this.staff.api;
  }

  async settings(): Promise<{ body: Record<string, unknown>; etag: string }> {
    const r = await this.api.get<Record<string, unknown>>(`${this.base}/settings`);
    expect(r.status, `read ${this.store.name}'s settings`).toBe(200);
    return { body: r.body, etag: r.etag };
  }

  async patchSettings(patch: Record<string, unknown>): Promise<void> {
    const { etag } = await this.settings();
    await this.api.ok(`change ${this.store.name}'s settings`, 'PATCH', `${this.base}/settings`, patch, { ifMatch: etag });
  }

  /** Runs fn with the settings in patch, then puts the earlier values back. */
  async withSettings<T>(patch: Record<string, unknown>, fn: () => Promise<T>): Promise<T> {
    const { body } = await this.settings();
    const before = Object.fromEntries(Object.keys(patch).map((k) => [k, body[k]]));
    await this.patchSettings(patch);
    try {
      return await fn();
    } finally {
      await this.patchSettings(before);
    }
  }

  order(id: string) {
    return this.api.ok<StoreOrder>('shop reads the order', 'GET', `${this.base}/orders/${id}`);
  }

  accept(id: string, lines?: { orderItemId: string; quantity: number }[]) {
    return this.api.ok<Order>('shop accepts', 'POST', `${this.base}/orders/${id}/accept`, lines ? { lines } : {});
  }

  ready(id: string) {
    return this.api.ok<Order>('shop marks ready', 'POST', `${this.base}/orders/${id}/ready`);
  }

  dispatch(id: string) {
    return this.api.ok<StoreOrder>('shop sends the order out', 'POST', `${this.base}/orders/${id}/dispatch`);
  }

  /** Enters the customer's code at the counter (read from the customer's order, never shown). */
  handOver(id: string, code: string) {
    return this.api.ok<Order>('shop enters the pickup code', 'POST', `${this.base}/orders/${id}/verify-code`, { code, paymentMethod: 'cash' });
  }

  creditAccount() {
    return this.api.ok<CreditAccount>('shop reads its credit account', 'GET', `/v1/biz/businesses/${this.businessId}/credit-account`, undefined, { params: { limit: 50 } });
  }
}

/** A customer, through the customer API. */
export class Customer {
  constructor(readonly who: Principal) {}

  get api(): Api {
    return this.who.api;
  }

  quote(store: SeedStore, items: Item[], creditsToUse = 0) {
    return this.api.ok<Quote>('customer prices the cart', 'POST', '/v1/me/checkout/quote', { storeId: store.id, fulfilment: 'pickup', items, creditsToUse });
  }

  /** Places a pickup order at the quoted total. */
  async place(store: SeedStore, items: Item[], creditsToUse = 0): Promise<Order> {
    const q = await this.quote(store, items, creditsToUse);
    expect(q.orderable, 'the cart can be booked').toBe(true);
    return this.api.ok<Order>('customer places the order', 'POST', '/v1/me/orders', {
      storeId: store.id,
      fulfilment: 'pickup',
      items,
      creditsToUse,
      expectedTotalPaise: q.totalPaise,
    });
  }

  order(id: string) {
    return this.api.ok<Order>('customer reads the order', 'GET', `/v1/me/orders/${id}`);
  }

  /** The saved address with this label, added at `at` when there is none yet (reruns reuse it). */
  async savedAddress(label: string, at: { lat: number; lng: number }): Promise<Address> {
    const list = await this.api.ok<{ addresses: Address[] }>('customer lists addresses', 'GET', '/v1/me/addresses');
    const found = list.addresses.find((a) => a.label === label);
    if (found) return found;
    return this.api.ok<Address>('customer saves an address', 'POST', '/v1/me/addresses', {
      label,
      line1: '3, 12th Main Road',
      pincode: '560038',
      lat: at.lat,
      lng: at.lng,
    });
  }

  /** The pickup code, as the customer's order page shows it. Never print it. */
  async code(id: string): Promise<string> {
    const o = await this.order(id);
    expect(o.code ?? '', 'the order shows a 4-digit pickup code').toMatch(/^\d{4}$/);
    return o.code ?? '';
  }

  wallet() {
    return this.api.ok<Wallet>('customer reads the wallet', 'GET', '/v1/me/wallet');
  }

  me() {
    return this.api.ok<{ customer?: { activeOrderLimit: { maxActiveOrders: number; until: string; reason: string } | null } | null }>('customer reads /me', 'GET', '/v1/me');
  }
}

/** Place → accept → ready → code at the counter → collected. */
export async function placeAndCollect(c: Customer, shop: Shop, items: Item[], creditsToUse = 0): Promise<Order> {
  const placed = await c.place(shop.store, items, creditsToUse);
  expect(placed.status).toBe('placed');
  await shop.accept(placed.id);
  await shop.ready(placed.id);
  const done = await shop.handOver(placed.id, await c.code(placed.id));
  expect(done.status, 'the order is collected').toBe('collected');
  return c.order(placed.id);
}

/** Waits until the customer's order reaches one of the statuses (a worker job moves it). */
export async function waitForStatus(c: Customer, id: string, statuses: string[], timeoutMs = 40_000): Promise<Order> {
  let last: Order | undefined;
  await expect
    .poll(
      async () => {
        last = await c.order(id);
        return statuses.includes(last.status);
      },
      { timeout: timeoutMs, intervals: [500, 1000, 2000] },
    )
    .toBe(true);
  if (!last) throw new Error('no order read');
  return last;
}

/** Waits until the wallet's available credits equal want (ledger postings run in the worker). */
export async function waitForCredits(c: Customer, want: number, timeoutMs = 30_000): Promise<Wallet> {
  let last: Wallet | undefined;
  await expect
    .poll(
      async () => {
        last = await c.wallet();
        return last.availableCredits;
      },
      { timeout: timeoutMs, intervals: [500, 1000, 2000] },
    )
    .toBe(want);
  if (!last) throw new Error('no wallet read');
  return last;
}

/** The customer's counter code at the server time `at` (RFC 6238, SHA-1, 4 digits, 60 s; D-12). Never print it. */
export async function counterCode(c: Customer, at: number): Promise<string> {
  const seed = await c.api.ok<{ secret: string; digits: number; periodSeconds: number }>('customer reads the counter-code seed', 'GET', '/v1/me/counter-code-seed');
  return totp(seed.secret, at, seed.digits, seed.periodSeconds);
}

export { ulid };

/**
 * First-run picks in the customer app (C-01 language, C-02 place): English and the seed's QA point,
 * written to the app's own storage keys before any page script runs, so search, baskets and the
 * shop pages see nearby shops. A choice the journey makes later (a language switch) is kept.
 */
export async function customerFirstRun(page: Page) {
  await page.addInitScript((place: string) => {
    try {
      if (!localStorage.getItem('todayz.lang')) localStorage.setItem('todayz.lang', 'en');
      if (!localStorage.getItem('todayz.place')) localStorage.setItem('todayz.place', place);
    } catch {
      // storage blocked: the journey's own checks fail with the screen they see
    }
  }, JSON.stringify({ kind: 'gps', lat: QA_POINT.lat, lng: QA_POINT.lng }));
  await page.goto('/');
}

export interface AppPage {
  context: BrowserContext;
  page: Page;
  /** Takes the session back from the browser, then closes it. */
  close(): Promise<void>;
}

/** A browser context for one app, signed in as `who` with the principal's own session. */
export async function openApp(browser: Browser, who: Principal, baseURL: string): Promise<AppPage> {
  const context = await browser.newContext({ baseURL });
  await who.attach(context);
  const page = await context.newPage();
  return {
    context,
    page,
    close: async () => {
      await who.detach();
      await context.close();
    },
  };
}

/**
 * Waits until the customer app's saved cart (IndexedDB `todayz-customer`, customer/src/cart/storage.ts)
 * holds at least one line. The app writes after each change; a full page load before the write lands
 * aborts it, so a journey calls this before it navigates or reloads.
 */
export async function cartSaved(page: Page) {
  await expect
    .poll(
      () =>
        page.evaluate(
          () =>
            new Promise<number>((done) => {
              const req = indexedDB.open('todayz-customer');
              req.onerror = () => done(0);
              req.onsuccess = () => {
                const db = req.result;
                if (!db.objectStoreNames.contains('cart')) return done(0);
                const get = db.transaction('cart', 'readonly').objectStore('cart').get('current');
                get.onsuccess = () => done((get.result as { lines?: unknown[] } | undefined)?.lines?.length ?? 0);
                get.onerror = () => done(0);
              };
            }),
        ),
      { message: 'the cart is saved in the browser' },
    )
    .toBeGreaterThan(0);
}

/** C-09 → C-11 → C-12: checks out the cart for pickup and returns the new order's id. */
export async function checkoutCart(page: Page): Promise<string> {
  await cartSaved(page);
  if (new URL(page.url()).pathname !== '/cart') await page.goto('/cart');
  await expect(page.getByRole('heading', { level: 1, name: 'Cart' })).toBeVisible();
  await page.getByRole('button', { name: /Checkout/ }).first().click();
  await expect(page.getByRole('heading', { level: 1, name: 'Checkout' })).toBeVisible();
  await page.getByRole('button', { name: /^Place order/ }).click();
  await expect(page.getByTestId('placed-check')).toBeVisible();
  await expect(page).toHaveURL(/\/orders\/[0-9A-Z]{26}$/);
  return new URL(page.url()).pathname.split('/').pop() ?? '';
}

type People = typeof PEOPLE;

/** `test` with `who(key)`: the signed-in principal for a seed person, kept for the whole run. */
export const test = base.extend<{ who: (key: keyof People) => Promise<Principal> }>({
  who: async ({}, use) => {
    await use((key) => principal(PEOPLE[key]));
  },
});

export { expect, readState, URLS };
