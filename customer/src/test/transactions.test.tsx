import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import type { Order } from '@todayz/api-client/gen/model/order';
import type { OrderList } from '@todayz/api-client/gen/model/orderList';
import { formatMoney } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { addToCart, getCart, resetCartForTests } from '../cart/cart';
import { resetCreditsForTests } from '../cart/credits';
import { setStreamFetchForTests } from '../orders/stream';
import { json, problem, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const STORE = { id: '01J9ZSTOREAAAAAAAAAAAAAAAA', slug: 'raju-kirana', name: 'Raju Kirana' };
const ITEM = '01J9ZITEMAAAAAAAAAAAAAAAAA';
const ORDER_ID = '01J9ZORDERAAAAAAAAAAAAAAAA';
const NOW = '2026-10-23T10:00:00.000Z';
const at = (min: number) => new Date(Date.parse(NOW) + min * 60_000).toISOString();

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on = (method: string, path: string, make: (c: Call) => Response): Handler => (c) =>
  c.method === method && c.path === path ? make(c) : undefined;

function quote(over: Partial<CheckoutQuote> = {}, unit = 2500, availability: 'in_stock' | 'short' | 'out_of_stock' = 'in_stock'): CheckoutQuote {
  const total = unit * 2;
  return {
    storeId: STORE.id,
    fulfilment: 'pickup',
    lines: [
      {
        storeItemId: ITEM,
        productId: '01J9ZPRODAAAAAAAAAAAAAAAAA',
        name: 'Toor dal',
        variant: null,
        packSize: 1,
        unit: 'kg',
        imageKey: null,
        quantity: 2,
        availableQuantity: availability === 'short' ? 1 : 10,
        availability,
        unitPricePaise: unit,
        mrpPaise: unit,
        lineSubtotalPaise: total,
        discountPaise: 0,
        lineTotalPaise: total,
        priceSource: 'catalog',
        validUntil: null,
      },
    ],
    subtotalPaise: total,
    discountPaise: 0,
    itemSubtotalPaise: total,
    itemsAfterOffersPaise: total,
    deliveryFeePaise: 0,
    creditsRequested: 0,
    creditsAllowed: 0,
    creditsUsed: 0,
    creditsLimit: null,
    totalPaise: total,
    offersApplied: [],
    orderable: availability === 'in_stock',
    validUntil: null,
    serverNow: NOW,
    ...over,
  } as CheckoutQuote;
}

function order(over: Partial<Order> = {}): Order {
  return {
    id: ORDER_ID,
    number: 'R-1042',
    store: { ...STORE, addressLine: '12 Main Road', locality: 'Indiranagar', phone: null },
    fulfilment: 'pickup',
    status: 'placed',
    statusReason: null,
    statusNote: null,
    lines: [
      {
        id: '01J9ZLINEAAAAAAAAAAAAAAAAA',
        storeItemId: ITEM,
        name: 'Toor dal',
        variant: null,
        packSize: 1,
        unit: 'kg',
        imageKey: null,
        unitPricePaise: 2500,
        quantityRequested: 2,
        quantityAccepted: null,
        lineTotalPaise: 5000,
        priceSource: 'catalog',
      },
    ],
    subtotalPaise: 5000,
    discountPaise: 0,
    itemSubtotalPaise: 5000,
    itemsAfterOffersPaise: 5000,
    creditsUsed: 0,
    deliveryFeePaise: 0,
    totalPaise: 5000,
    cashbackCredits: 0,
    paymentStatus: 'pay_at_shop',
    paymentMethod: null,
    code: null,
    codeLocked: false,
    canExtend: false,
    askReplyId: null,
    acceptBy: at(4),
    partialConfirmBy: null,
    holdUntil: null,
    extendedAt: null,
    placedAt: NOW,
    acceptedAt: null,
    collectedAt: null,
    timeline: [{ fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW }],
    version: 1,
    serverNow: NOW,
    ...over,
  } as Order;
}

const accepted = (over: Partial<Order> = {}) =>
  order({
    status: 'accepted',
    code: '4821',
    acceptBy: null,
    holdUntil: at(30),
    canExtend: true,
    acceptedAt: at(1),
    timeline: [
      { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW },
      { fromStatus: 'placed', toStatus: 'accepted', actor: 'staff', reason: null, at: at(1) },
    ],
    version: 2,
    ...over,
  });

beforeEach(() => {
  resetCartForTests();
  resetCreditsForTests();
  addToCart(STORE, { storeItemId: ITEM, productName: 'Toor dal', pricePaise: 2500, mrpPaise: 2500, packLabel: '1 kg' }, 2);
});
// The live stream never reaches a real network: by default it is a connection that stays quiet.
const quietStream = (() => Promise.resolve(new Response(new ReadableStream<Uint8Array>({ start: () => undefined }), { status: 200, headers: { 'content-type': 'text/event-stream' } }))) as typeof fetch;
beforeEach(() => setStreamFetchForTests(quietStream));
afterEach(() => setStreamFetchForTests(undefined));

describe('C-09 cart', () => {
  it('lists lines, flags a short line and offers to match the stock', async () => {
    renderAt('/cart', { returning: true, handlers: [signedIn, on('POST', '/v1/me/checkout/quote', () => json(200, quote({}, 2500, 'short')))] });
    expect(await screen.findByText('Toor dal')).toBeInTheDocument();
    expect(await screen.findByText('Only 1 left')).toBeInTheDocument();
    expect(screen.getByText('Remove or reduce the items marked below to continue.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Checkout' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Reduce to 1' }));
    await waitFor(() => expect(getCart().lines[0]?.qty).toBe(1));
  });

  it('shows the total and a Checkout button when everything is available', async () => {
    renderAt('/cart', { returning: true, handlers: [signedIn, on('POST', '/v1/me/checkout/quote', () => json(200, quote()))] });
    expect(await screen.findByRole('button', { name: /Checkout/ })).toBeInTheDocument();
    expect(screen.getAllByText(formatMoney(5000)).length).toBeGreaterThan(0);
    expect(screen.getByText('Pick up at the shop and pay there.')).toBeInTheDocument();
  });

  it('shows an empty state with no lines', async () => {
    resetCartForTests();
    renderAt('/cart', { returning: true, handlers: [signedIn] });
    expect(await screen.findByText('Your cart is empty')).toBeInTheDocument();
  });
});

/** A quote that honours `creditsToUse` up to `max` credits, like the server (01 §7). */
function creditsQuote(max: number, limit: CheckoutQuote['creditsLimit'] = null, seen: number[] = []): Handler {
  return on('POST', '/v1/me/checkout/quote', (c) => {
    const asked = (c.body as { creditsToUse: number }).creditsToUse;
    seen.push(asked);
    const used = Math.min(asked, max);
    return json(
      200,
      quote({
        creditsRequested: asked,
        creditsAllowed: max,
        creditsUsed: used,
        creditsLimit: used < asked ? limit ?? 'credit_max_exceeded' : null,
        totalPaise: 5000 - used * 100,
      }),
    );
  });
}

describe('credits at cart and checkout', () => {
  it('turns credits on at the most the quote allows and shows the reduced total', async () => {
    const seen: number[] = [];
    renderAt('/cart', { returning: true, handlers: [signedIn, creditsQuote(30, null, seen)] });
    const toggle = await screen.findByRole('switch', { name: 'Use credits' });
    expect(screen.getByText(/up to 30 credits/)).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(await screen.findByText(/Using 30 credits for ₹30 off/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText(formatMoney(2000)).length).toBeGreaterThan(0));
    expect(seen).toContain(30);
  });

  it('sends the chosen credits with the order and the total the person saw', async () => {
    const posts: Call[] = [];
    const { router } = renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        creditsQuote(30),
        on('POST', '/v1/me/orders', (c) => {
          posts.push(c);
          return json(201, order());
        }),
        on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order())),
      ],
    });
    fireEvent.click(await screen.findByRole('switch', { name: 'Use credits' }));
    fireEvent.change(await screen.findByRole('slider', { name: 'Credits to use' }), { target: { value: '12' } });
    const place = await screen.findByRole('button', { name: `Place order · ${formatMoney(3800)}` }, { timeout: 3000 });
    await waitFor(() => expect(place).toBeEnabled());
    fireEvent.click(place);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/orders/${ORDER_ID}`));
    expect(posts[0]?.body).toMatchObject({ creditsToUse: 12, expectedTotalPaise: 3800 });
  });

  it('shows the monthly cap reason with the switch off and disabled, and still places without credits', async () => {
    renderAt('/cart', { returning: true, handlers: [signedIn, creditsQuote(0, 'credit_monthly_cap')] });
    const toggle = await screen.findByRole('switch', { name: 'Use credits' });
    expect(toggle).toBeDisabled();
    expect(toggle).not.toBeChecked();
    expect(screen.getByText("You've used this month's credit limit.")).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Checkout/ })).toBeInTheDocument();
  });

  it("says credits can't be used at this shop today for the store's daily cap", async () => {
    renderAt('/cart', { returning: true, handlers: [signedIn, creditsQuote(0, 'store_credit_cap')] });
    expect(await screen.findByText("Credits can't be used at this shop today.")).toBeInTheDocument();
  });

  it('hides the control when there is no balance', async () => {
    renderAt('/cart', { returning: true, handlers: [signedIn, creditsQuote(0, 'no_balance')] });
    expect(await screen.findByRole('button', { name: /Checkout/ })).toBeInTheDocument();
    expect(screen.queryByRole('switch', { name: 'Use credits' })).toBeNull();
  });
});

describe('C-11 checkout', () => {
  it('sends a signed-out person to sign in first and keeps the cart', async () => {
    const { router } = renderAt('/checkout');
    await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
    expect(router.state.location.search).toMatchObject({ next: '/checkout' });
    expect(getCart().lines).toHaveLength(1);
  });

  it('places the order once with an idempotency key and opens the tracking page', async () => {
    const posts: Call[] = [];
    const { router } = renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        on('POST', '/v1/me/checkout/quote', () => json(200, quote())),
        on('POST', '/v1/me/orders', (c) => {
          posts.push(c);
          return json(201, order());
        }),
        on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order())),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: `Place order · ${formatMoney(5000)}` }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/orders/${ORDER_ID}`));
    expect(posts).toHaveLength(1);
    expect(posts[0]?.headers.get('idempotency-key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(posts[0]?.body).toMatchObject({ storeId: STORE.id, fulfilment: 'pickup', expectedTotalPaise: 5000, creditsToUse: 0 });
    await waitFor(() => expect(getCart().lines).toHaveLength(0));
    // The check animation shows once and the flag leaves the URL.
    expect(await screen.findByTestId('placed-check')).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it('reuses the same idempotency key when the same order is retried after a failure', async () => {
    const keys: (string | null)[] = [];
    renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        on('POST', '/v1/me/checkout/quote', () => json(200, quote())),
        on('POST', '/v1/me/orders', (c) => {
          keys.push(c.headers.get('idempotency-key'));
          return keys.length === 1 ? problem(503, 'unavailable') : json(201, order());
        }),
        on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order())),
      ],
    });
    const place = `Place order · ${formatMoney(5000)}`;
    fireEvent.click(await screen.findByRole('button', { name: place }));
    expect(await screen.findByText(/No connection/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: place }));
    await waitFor(() => expect(keys).toHaveLength(2));
    expect(keys[0]).toBeTruthy();
    expect(keys[1]).toBe(keys[0]);
  });

  it('needs a second tap after the price changed, then sends the new total', async () => {
    const bodies: unknown[] = [];
    const fresh = quote({}, 2700);
    renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        on('POST', '/v1/me/checkout/quote', () => json(200, quote())),
        on('POST', '/v1/me/orders', (c) => {
          bodies.push(c.body);
          if (bodies.length === 1) {
            return json(409, { type: 'about:blank#price_changed', title: 'x', status: 409, code: 'price_changed', extensions: { quote: fresh } });
          }
          return json(201, order());
        }),
        on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, order())),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: `Place order · ${formatMoney(5000)}` }));
    expect(await screen.findByText('The total changed')).toBeInTheDocument();
    const accept = await screen.findByRole('button', { name: `Accept new total · ${formatMoney(5400)}` });
    fireEvent.click(accept);
    expect(bodies).toHaveLength(1); // nothing sent by the acceptance tap
    fireEvent.click(await screen.findByRole('button', { name: `Place order · ${formatMoney(5400)}` }));
    await waitFor(() => expect(bodies).toHaveLength(2));
    expect(bodies[1]).toMatchObject({ expectedTotalPaise: 5400 });
  });

  it('names the line that has too little stock and keeps the cart', async () => {
    renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        on('POST', '/v1/me/checkout/quote', () => json(200, quote())),
        on('POST', '/v1/me/orders', () =>
          json(409, { type: 'about:blank#insufficient_stock', title: 'x', status: 409, code: 'insufficient_stock', extensions: { storeItemId: ITEM, available: 1 } }),
        ),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: `Place order · ${formatMoney(5000)}` }));
    expect(await screen.findByText(/Toor dal: only 1 left\./)).toBeInTheDocument();
    expect(getCart().lines).toHaveLength(1);
  });

  it('says the shop is closed and keeps the cart', async () => {
    renderAt('/checkout', {
      returning: true,
      handlers: [signedIn, on('POST', '/v1/me/checkout/quote', () => problem(409, 'store_closed'))],
    });
    expect((await screen.findAllByText(/Your cart is kept\./)).length).toBeGreaterThan(0);
    expect(getCart().lines).toHaveLength(1);
  });

  it('holds the update toast while checkout is open', async () => {
    const { update } = renderAt('/checkout', {
      returning: true,
      handlers: [signedIn, on('POST', '/v1/me/checkout/quote', () => json(200, quote()))],
    });
    await screen.findByRole('button', { name: /Place order/ });
    expect(update.getState().holds).toBe(1);
  });
});

describe('C-21 order limit', () => {
  const until = () => new Date(Date.now() + 7 * 24 * 60 * 60_000).toISOString();
  const meWith = (limit: unknown): Handler =>
    on('GET', '/v1/me', () =>
      json(200, {
        id: USER,
        name: null,
        preferredLang: 'en',
        phoneMasked: '90•••••011',
        app: 'customer',
        customer: { referralCode: 'ABC123', activeOrderLimit: limit },
        createdAt: NOW,
      }),
    );

  it('shows the limit and its end date on checkout while it lasts', async () => {
    renderAt('/checkout', {
      returning: true,
      handlers: [signedIn, meWith({ maxActiveOrders: 1, until: until(), reason: 'no_show_strikes' }), on('POST', '/v1/me/checkout/quote', () => json(200, quote()))],
    });
    expect(await screen.findByText('Ordering is limited for now')).toBeInTheDocument();
    expect(screen.getByText(/You missed several pickups, so until .+ you can have one open order at a time\./)).toBeInTheDocument();
  });

  it('shows the limit on Home too', async () => {
    renderAt('/', { returning: true, handlers: [signedIn, meWith({ maxActiveOrders: 1, until: until(), reason: 'no_show_strikes' })] });
    expect(await screen.findByText('Ordering is limited for now')).toBeInTheDocument();
  });

  it('shows nothing once the limit has ended', async () => {
    const calls: Call[] = [];
    renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        (c) => {
          calls.push(c);
          return undefined;
        },
        meWith({ maxActiveOrders: 1, until: new Date(Date.now() - 60_000).toISOString(), reason: 'no_show_strikes' }),
        on('POST', '/v1/me/checkout/quote', () => json(200, quote())),
      ],
    });
    expect(await screen.findByRole('button', { name: /Place order/ })).toBeInTheDocument();
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/me')).toBe(true));
    expect(screen.queryByText('Ordering is limited for now')).toBeNull();
  });

  it('gives the end date when the quote is refused with ordering_suspended', async () => {
    renderAt('/cart', {
      returning: true,
      handlers: [
        signedIn,
        on('POST', '/v1/me/checkout/quote', () =>
          json(409, { type: 'about:blank#ordering_suspended', title: 'ordering_suspended', status: 409, code: 'ordering_suspended', extensions: { until: until() } }),
        ),
      ],
    });
    expect(await screen.findByText(/until .+ you can have only one open order\. Finish or cancel it first\./)).toBeInTheDocument();
  });
});

describe('C-12 order tracking', () => {
  const track = (o: Order | (() => Order), extra: Handler[] = []) =>
    renderAt(`/orders/${ORDER_ID}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, typeof o === 'function' ? o() : o)), ...extra],
    });

  it('counts down to the shop answer from the server time', async () => {
    track(order());
    expect(await screen.findByText('Waiting for Raju Kirana to accept · 4:00 left')).toBeInTheDocument();
    expect(screen.queryByText('4821')).toBeNull();
  });

  it('shows the pickup code and the hold countdown once accepted', async () => {
    const { container } = track(accepted());
    expect(await screen.findByText('Pickup code')).toBeInTheDocument();
    expect(container.textContent).toContain('4821');
    expect(screen.getByText(/Held for you · 30:00 left/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: "I'm on my way (+10 min)" })).toBeInTheDocument();
  });

  it('extends the hold once with an idempotency key', async () => {
    const calls: Call[] = [];
    track(accepted(), [
      on('POST', `/v1/me/orders/${ORDER_ID}/extend`, (c) => {
        calls.push(c);
        return json(200, accepted({ canExtend: false, holdUntil: at(40), extendedAt: at(2), version: 3 }));
      }),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: "I'm on my way (+10 min)" }));
    expect(await screen.findByText('Added 10 minutes.')).toBeInTheDocument();
    expect(calls[0]?.headers.get('idempotency-key')).toBeTruthy();
    expect(screen.queryByRole('button', { name: "I'm on my way (+10 min)" })).toBeNull();
  });

  it('cancels with a reason; "something else" needs a note', async () => {
    const calls: Call[] = [];
    track(order(), [
      on('POST', `/v1/me/orders/${ORDER_ID}/cancel`, (c) => {
        calls.push(c);
        return json(200, order({ status: 'cancelled', statusReason: 'other', acceptBy: null, version: 2 }));
      }),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel order' }));
    const sheet = await screen.findByRole('dialog');
    fireEvent.click(within(sheet).getByLabelText('Something else'));
    const confirm = within(sheet).getByRole('button', { name: 'Cancel order' });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(sheet).getByLabelText('Tell us what happened'), { target: { value: 'Plans changed' } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toEqual({ reason: 'other', note: 'Plans changed' });
    expect(await screen.findByText('This order was cancelled')).toBeInTheDocument();
  });

  it('offers a new code when the code is locked', async () => {
    track(accepted({ code: null, codeLocked: true }));
    expect(await screen.findByText('Code locked')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Get a new code' })).toBeInTheDocument();
  });

  it('shows the outcome and a next step when the shop declined', async () => {
    track(order({ status: 'rejected', statusReason: 'out_of_stock', acceptBy: null, version: 2 }));
    expect(await screen.findByText('Raju Kirana could not take this order')).toBeInTheDocument();
    expect(screen.getByText('Some items were out of stock.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Find another shop' })).toBeInTheDocument();
  });

  describe('partial confirm', () => {
    const partial = (over: Partial<Order> = {}) => {
      const [first] = order().lines;
      return order({
        status: 'awaiting_customer',
        acceptBy: null,
        partialConfirmBy: at(3),
        lines: [{ ...first, quantityAccepted: 1, lineTotalPaise: 2500 } as Order['lines'][number]],
        subtotalPaise: 2500,
        itemSubtotalPaise: 2500,
        itemsAfterOffersPaise: 2500,
        totalPaise: 2500,
        version: 2,
        ...over,
      });
    };

    it('shows the changed line, the new total and a countdown from the server time', async () => {
      track(partial());
      expect(await screen.findByText('Raju Kirana changed your order')).toBeInTheDocument();
      expect(screen.getByText('1 instead of 2')).toBeInTheDocument();
      expect(screen.getByText('Confirm within 3:00')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: `Take it · ${formatMoney(2500)}` })).toBeInTheDocument();
    });

    it('shows the new items total, the re-capped credits and the total', async () => {
      track(partial({ creditsUsed: 5, totalPaise: 2000 }));
      const totals = (await screen.findByText('Items')).closest('dl') as HTMLElement;
      expect(within(totals).getByText(formatMoney(2500))).toBeInTheDocument();
      expect(within(totals).getByText('Credits used')).toBeInTheDocument();
      expect(within(totals).getByText(`−${formatMoney(500)}`)).toBeInTheDocument();
      expect(within(totals).getByText(formatMoney(2000))).toBeInTheDocument();
      expect(within(totals).queryByText('Delivery fee')).toBeNull();
    });

    it('says a line is not available when the shop accepted none of it', async () => {
      const [first] = order().lines;
      track(partial({ lines: [{ ...first, quantityAccepted: 0, lineTotalPaise: 0 } as Order['lines'][number]] }));
      expect(await screen.findByText('Not available. You asked for 2.')).toBeInTheDocument();
    });

    it('takes the changed order once with an idempotency key', async () => {
      const calls: Call[] = [];
      let current = partial();
      track(() => current, [
        on('POST', `/v1/me/orders/${ORDER_ID}/confirm-partial`, (c) => {
          calls.push(c);
          current = accepted({ totalPaise: 2500, version: 3 });
          // Like the server: an action answer never carries the pickup code, only a GET does.
          return json(200, { ...current, code: null });
        }),
      ]);
      fireEvent.click(await screen.findByRole('button', { name: `Take it · ${formatMoney(2500)}` }));
      expect(await screen.findByText('Pickup code')).toBeInTheDocument();
      expect(calls).toHaveLength(1);
      expect(calls[0]?.body).toEqual({ accept: true });
      expect(calls[0]?.headers.get('idempotency-key')).toBeTruthy();
    });

    it('cancels the order when the customer declines the change', async () => {
      const calls: Call[] = [];
      let current = partial();
      track(() => current, [
        on('POST', `/v1/me/orders/${ORDER_ID}/confirm-partial`, (c) => {
          calls.push(c);
          current = order({ status: 'cancelled', statusReason: 'declined_partial', acceptBy: null, version: 3 });
          return json(200, current);
        }),
      ]);
      fireEvent.click(await screen.findByRole('button', { name: 'Cancel order' }));
      expect(await screen.findByText('You cancelled the changed order.')).toBeInTheDocument();
      expect(calls[0]?.body).toEqual({ accept: false });
    });

    it('explains a 409 and keeps the page', async () => {
      track(partial(), [on('POST', `/v1/me/orders/${ORDER_ID}/confirm-partial`, () => problem(409, 'invalid_state'))]);
      fireEvent.click(await screen.findByRole('button', { name: `Take it · ${formatMoney(2500)}` }));
      expect(await screen.findByText('This order has changed again. The page shows the latest.')).toBeInTheDocument();
    });
  });

  it('moves when the live stream says the order changed', async () => {
    let current = order();
    const enc = new TextEncoder();
    setStreamFetchForTests((() => {
      const body = new ReadableStream<Uint8Array>({
        start(ctrl) {
          setTimeout(() => {
            current = accepted();
            ctrl.enqueue(enc.encode(`event: order.updated\nid: 1\ndata: {"type":"order.updated","id":"${ORDER_ID}","version":2}\n\n`));
          }, 30);
        },
      });
      return Promise.resolve(new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } }));
    }) as typeof fetch);
    renderAt(`/orders/${ORDER_ID}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, current))],
    });
    expect(await screen.findByText(/Waiting for Raju Kirana to accept/)).toBeInTheDocument();
    expect(await screen.findByText('Pickup code', undefined, { timeout: 3000 })).toBeInTheDocument();
  });
});

describe('C-13 orders list', () => {
  const summary = (id: string, status: Order['status']) => ({
    id,
    number: `R-${id.slice(-2)}`,
    store: { ...STORE, addressLine: '12 Main Road', locality: null, phone: null },
    fulfilment: 'pickup' as const,
    status,
    statusReason: null,
    itemCount: 2,
    totalPaise: 5000,
    acceptBy: null,
    holdUntil: null,
    placedAt: NOW,
    version: 1,
  });
  const list = (items: ReturnType<typeof summary>[]): OrderList => ({ items, nextCursor: null, serverNow: NOW });

  it('shows active orders first, then history', async () => {
    const asked: string[] = [];
    renderAt('/orders', {
      returning: true,
      handlers: [
        signedIn,
        (c: Call) => {
          if (c.method !== 'GET' || c.path !== '/v1/me/orders') return undefined;
          asked.push(c.search);
          return json(200, c.search.includes('status=active') ? list([summary('01J9ZORDERAAAAAAAAAAAAAA01', 'accepted')]) : list([summary('01J9ZORDERAAAAAAAAAAAAAA02', 'collected')]));
        },
      ],
    });
    const active = await screen.findByRole('region', { name: 'In progress' });
    const past = await screen.findByRole('region', { name: 'Earlier' });
    expect(within(active).getByText('Accepted')).toBeInTheDocument();
    expect(within(past).getByText('Collected')).toBeInTheDocument();
    expect(active.compareDocumentPosition(past) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(asked.some((q) => q.includes('status=active'))).toBe(true);
    expect(asked.some((q) => q.includes('status=past'))).toBe(true);
  });

  it('shows an empty state and a placeholder In store tab', async () => {
    renderAt('/orders', {
      returning: true,
      handlers: [signedIn, on('GET', '/v1/me/orders', () => json(200, list([])))],
    });
    expect(await screen.findByText('No orders yet')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'In store' }));
    expect(await screen.findByText('Store bills are coming')).toBeInTheDocument();
  });

  it('asks the signed-out person to sign in', async () => {
    renderAt('/orders');
    expect(await screen.findByText('Sign in to see your orders')).toBeInTheDocument();
  });
});
