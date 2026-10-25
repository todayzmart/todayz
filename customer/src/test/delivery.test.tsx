// W2-15 customer delivery: the C-09 switch, C-11 address and refusals, C-12 delivery code and the
// C-18 saved addresses (01 §5.5, D-94, D-219).
import { ApiError } from '@todayz/api-client';
import type { Address } from '@todayz/api-client/gen/model/address';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import type { Order } from '@todayz/api-client/gen/model/order';
import { formatDistance, formatMoney } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { addToCart, resetCartForTests } from '../cart/cart';
import { resetCreditsForTests } from '../cart/credits';
import { deliveryProblemOf } from '../cart/deliveryRules';
import { inRadius, resetFulfilmentForTests } from '../cart/fulfilment';
import { setStreamFetchForTests } from '../orders/stream';
import { page } from './discover-fixtures';
import { json, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const STORE = { id: '01J9ZSTOREAAAAAAAAAAAAAAAA', slug: 'raju-kirana', name: 'Raju Kirana' };
const ITEM = '01J9ZITEMAAAAAAAAAAAAAAAAA';
const ORDER_ID = '01J9ZORDERAAAAAAAAAAAAAAAA';
const NOW = '2026-10-23T10:00:00.000Z';
const at = (min: number) => new Date(Date.parse(NOW) + min * 60_000).toISOString();

const HOME = '01J9ZADDRHOMEAAAAAAAAAAAAA';
const FAR = '01J9ZADDRFARAAAAAAAAAAAAAA';

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on = (method: string, path: string, make: (c: Call) => Response): Handler => (c) =>
  c.method === method && c.path === path ? make(c) : undefined;

// The shop sits at 12.97, 77.64 and delivers within 3 km for ₹20, minimum ₹100, free on ₹500+.
const shop = page({ storeId: STORE.id, storeSlug: STORE.slug, name: STORE.name, deliveryFreeAbovePaise: 50000 });
const storePage: Handler = on('GET', `/v1/discover/stores/${STORE.slug}`, () => json(200, shop));

function address(over: Partial<Address> = {}): Address {
  return {
    addressId: HOME,
    label: 'Home',
    line1: '4 Lake View',
    line2: null,
    landmark: null,
    pincode: '560038',
    lat: 12.971,
    lng: 77.641,
    isDefault: true,
    updatedAt: NOW,
    version: 1,
    ...over,
  };
}
// About 11 km north of the shop.
const far = address({ addressId: FAR, label: 'Office', line1: '9 Hebbal Road', lat: 13.07, lng: 77.64, isDefault: false });

const addresses = (list: Address[]): Handler => on('GET', '/v1/me/addresses', () => json(200, { addresses: list }));

function quote(over: Partial<CheckoutQuote> = {}): CheckoutQuote {
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
        availableQuantity: 10,
        availability: 'in_stock',
        unitPricePaise: 2500,
        mrpPaise: 2500,
        lineSubtotalPaise: 5000,
        discountPaise: 0,
        lineTotalPaise: 5000,
        priceSource: 'catalog',
        validUntil: null,
      },
    ],
    subtotalPaise: 5000,
    discountPaise: 0,
    itemSubtotalPaise: 5000,
    itemsAfterOffersPaise: 5000,
    deliveryFeePaise: 0,
    creditsRequested: 0,
    creditsAllowed: 0,
    creditsUsed: 0,
    creditsLimit: null,
    totalPaise: 5000,
    offersApplied: [],
    orderable: true,
    validUntil: null,
    serverNow: NOW,
    ...over,
  } as CheckoutQuote;
}

/** The quote as the server answers it: delivery adds the ₹20 fee. */
const quoting = (seen: Call[] = []): Handler =>
  on('POST', '/v1/me/checkout/quote', (c) => {
    seen.push(c);
    const b = c.body as { fulfilment: string };
    return json(200, b.fulfilment === 'delivery' ? quote({ fulfilment: 'delivery', deliveryFeePaise: 2000, totalPaise: 7000 }) : quote());
  });

function deliveryOrder(over: Partial<Order> = {}): Order {
  return {
    id: ORDER_ID,
    number: 'R-1042',
    store: { ...STORE, addressLine: '12 Main Road', locality: 'Indiranagar', phone: null },
    fulfilment: 'delivery',
    status: 'accepted',
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
        quantityAccepted: 2,
        lineTotalPaise: 5000,
        priceSource: 'catalog',
      },
    ],
    subtotalPaise: 5000,
    discountPaise: 0,
    itemSubtotalPaise: 5000,
    itemsAfterOffersPaise: 5000,
    creditsUsed: 0,
    deliveryFeePaise: 2000,
    totalPaise: 7000,
    cashbackCredits: 0,
    paymentStatus: 'pay_at_shop',
    paymentMethod: null,
    code: '4821',
    codeLocked: false,
    canExtend: false,
    askReplyId: null,
    acceptBy: null,
    partialConfirmBy: null,
    holdUntil: null,
    extendedAt: null,
    dispatchBy: at(30),
    deliveryAddress: { label: 'Home', line1: '4 Lake View', line2: null, landmark: null, pincode: '560038', lat: 12.971, lng: 77.641 },
    placedAt: NOW,
    acceptedAt: at(1),
    collectedAt: null,
    timeline: [
      { fromStatus: null, toStatus: 'placed', actor: 'customer', reason: null, at: NOW },
      { fromStatus: 'placed', toStatus: 'accepted', actor: 'staff', reason: null, at: at(1) },
    ],
    version: 2,
    serverNow: NOW,
    ...over,
  } as Order;
}

const delivery = () => screen.findByRole('radio', { name: 'Delivery' });

beforeEach(() => {
  resetCartForTests();
  resetCreditsForTests();
  resetFulfilmentForTests();
  addToCart(STORE, { storeItemId: ITEM, productName: 'Toor dal', pricePaise: 2500, mrpPaise: 2500, packLabel: '1 kg' }, 2);
});
const quietStream = (() => Promise.resolve(new Response(new ReadableStream<Uint8Array>({ start: () => undefined }), { status: 200, headers: { 'content-type': 'text/event-stream' } }))) as typeof fetch;
beforeEach(() => setStreamFetchForTests(quietStream));
afterEach(() => setStreamFetchForTests(undefined));

describe('delivery rules', () => {
  it('counts an address on the radius as inside, like the server', () => {
    const store = { lat: 12.97, lng: 77.64, deliveryRadiusM: 3000 };
    expect(inRadius(store, { lat: 12.97, lng: 77.64 })).toBe(true);
    // 0.0269° of latitude is about 2.99 km; 0.028° about 3.11 km.
    expect(inRadius(store, { lat: 12.9969, lng: 77.64 })).toBe(true);
    expect(inRadius(store, { lat: 12.998, lng: 77.64 })).toBe(false);
    expect(inRadius({ ...store, deliveryRadiusM: 0 }, { lat: 12.97, lng: 77.64 })).toBe(false);
  });

  it('reads the delivery refusals and nothing else', () => {
    const err = (code: string, extensions: Record<string, unknown>) =>
      new ApiError({ type: 'about:blank', title: code, status: 409, code, extensions }, new Headers());
    expect(deliveryProblemOf(err('delivery_closed', { nextDeliveryWindow: { opensAt: at(60), closesAt: at(600) } }))).toEqual({
      kind: 'delivery_closed',
      next: { opensAt: at(60), closesAt: at(600) },
    });
    expect(deliveryProblemOf(err('delivery_closed', { nextDeliveryWindow: null }))).toEqual({ kind: 'delivery_closed', next: null });
    expect(deliveryProblemOf(err('below_minimum', { minimumPaise: 10000, itemSubtotalPaise: 5000 }))).toEqual({
      kind: 'below_minimum',
      minimumPaise: 10000,
      itemSubtotalPaise: 5000,
    });
    expect(deliveryProblemOf(err('store_not_bookable', { reason: 'outside_delivery_radius' }))).toEqual({ kind: 'outside_radius' });
    expect(deliveryProblemOf(err('store_not_bookable', { reason: 'paused' }))).toBeNull();
    expect(deliveryProblemOf(err('store_closed', {}))).toBeNull();
    expect(deliveryProblemOf(new Error('x'))).toBeNull();
  });
});

describe('C-09 cart delivery', () => {
  it('shows the terms and quotes delivery to the default address inside the radius', async () => {
    const seen: Call[] = [];
    renderAt('/cart', { returning: true, handlers: [signedIn, storePage, addresses([far, address()]), quoting(seen)] });
    fireEvent.click(await delivery());
    expect(
      await screen.findByText(
        `${formatMoney(2000)} delivery · Minimum ${formatMoney(10000)} · Free delivery on ${formatMoney(50000)}+ · Delivers within ${formatDistance(3000)}`,
      ),
    ).toBeInTheDocument();
    expect(await screen.findByText('Deliver to Home: 4 Lake View, 560038')).toBeInTheDocument();
    await waitFor(() => expect(seen.some((c) => (c.body as { fulfilment: string }).fulfilment === 'delivery')).toBe(true));
    expect(seen.at(-1)?.body).toMatchObject({ fulfilment: 'delivery', addressId: HOME });
    expect(await screen.findByText(`Add ${formatMoney(45000)} more for free delivery`)).toBeInTheDocument();
    expect(screen.getAllByText(formatMoney(2000)).length).toBeGreaterThan(0);
  });

  it('stays priced for pickup when no saved address is inside the radius', async () => {
    const seen: Call[] = [];
    renderAt('/cart', { returning: true, handlers: [signedIn, storePage, addresses([far]), quoting(seen)] });
    fireEvent.click(await delivery());
    expect(await screen.findByText('None of your addresses is inside the delivery area.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Add an address' })).toHaveAttribute('href', '/checkout');
    expect(seen.every((c) => (c.body as { fulfilment: string }).fulfilment === 'pickup')).toBe(true);
  });

  it('offers no switch when the shop does not deliver', async () => {
    renderAt('/cart', {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/discover/stores/${STORE.slug}`, () => json(200, { ...shop, delivery: false })), quoting()],
    });
    expect(await screen.findByText('Pick up at the shop and pay there.')).toBeInTheDocument();
    expect(screen.queryByRole('radio', { name: 'Delivery' })).toBeNull();
  });

  it('says the minimum and switches back to pickup', async () => {
    const seen: Call[] = [];
    renderAt('/cart', {
      returning: true,
      handlers: [
        signedIn,
        storePage,
        addresses([address()]),
        on('POST', '/v1/me/checkout/quote', (c) => {
          seen.push(c);
          return (c.body as { fulfilment: string }).fulfilment === 'delivery'
            ? json(409, { type: 'about:blank', title: 'below_minimum', status: 409, code: 'below_minimum', extensions: { minimumPaise: 10000, itemSubtotalPaise: 5000 } })
            : json(200, quote());
        }),
      ],
    });
    fireEvent.click(await delivery());
    expect(await screen.findByText(`Delivery needs a minimum of ${formatMoney(10000)}`)).toBeInTheDocument();
    expect(screen.getByText(`Add ${formatMoney(5000)} more to get it delivered, or pick it up.`)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Switch to pickup' }));
    await waitFor(() => expect(screen.getByRole('radio', { name: 'Pickup' })).toHaveAttribute('aria-checked', 'true'));
    expect(await screen.findByRole('button', { name: /Checkout/ })).toBeInTheDocument();
  });
});

describe('C-11 checkout delivery', () => {
  it('lists the addresses, disables one outside the radius and places with the address id', async () => {
    const posts: Call[] = [];
    const { router } = renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        storePage,
        addresses([address(), far]),
        quoting(),
        on('POST', '/v1/me/orders', (c) => {
          posts.push(c);
          return json(201, deliveryOrder({ status: 'placed', code: null }));
        }),
        on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, deliveryOrder({ status: 'placed', code: null }))),
      ],
    });
    fireEvent.click(await delivery());
    const list = await screen.findByRole('group', { name: 'Deliver to' });
    expect(within(list).getByRole('radio', { name: /Home/ })).toBeChecked();
    expect(within(list).getByRole('radio', { name: /Office/ })).toBeDisabled();
    expect(within(list).getByText(/Outside the delivery area/)).toBeInTheDocument();
    expect(await screen.findByText('You pay when it arrives: cash or UPI. Nothing is charged now.')).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: `Place order · ${formatMoney(7000)}` }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/orders/${ORDER_ID}`));
    expect(posts).toHaveLength(1);
    expect(posts[0]?.body).toMatchObject({ storeId: STORE.id, fulfilment: 'delivery', addressId: HOME, expectedTotalPaise: 7000 });
  });

  it('says when delivery opens next and switches to pickup', async () => {
    const seen: Call[] = [];
    renderAt('/checkout', {
      returning: true,
      handlers: [
        signedIn,
        storePage,
        addresses([address()]),
        on('POST', '/v1/me/checkout/quote', (c) => {
          seen.push(c);
          return (c.body as { fulfilment: string }).fulfilment === 'delivery'
            ? json(409, {
                type: 'about:blank',
                title: 'delivery_closed',
                status: 409,
                code: 'delivery_closed',
                // 10:30–20:30 IST tomorrow.
                extensions: { nextDeliveryWindow: { opensAt: '2026-10-24T05:00:00.000Z', closesAt: '2026-10-24T15:00:00.000Z' } },
              })
            : json(200, quote());
        }),
      ],
    });
    fireEvent.click(await delivery());
    expect(await screen.findByText('Delivery is closed now')).toBeInTheDocument();
    expect(screen.getByText(/Next delivery: tomorrow 10:30/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Place order/ })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Switch to pickup' }));
    await waitFor(() => expect(screen.getByRole('button', { name: `Place order · ${formatMoney(5000)}` })).toBeEnabled());
    expect(screen.queryByText('Delivery is closed now')).toBeNull();
    expect(seen.at(-1)?.body).toMatchObject({ fulfilment: 'pickup' });
  });
});

describe('C-12 delivery tracking', () => {
  const track = (o: Order) =>
    renderAt(`/orders/${ORDER_ID}`, { returning: true, handlers: [signedIn, on('GET', `/v1/me/orders/${ORDER_ID}`, () => json(200, o))] });

  it('keeps the code back until the order is out for delivery', async () => {
    const { container } = track(deliveryOrder());
    expect(await screen.findByText(/Raju Kirana accepted your order and will send it out by/)).toBeInTheDocument();
    expect(screen.getByText('Delivering to Home: 4 Lake View, 560038')).toBeInTheDocument();
    expect(container.textContent).not.toContain('4821');
  });

  it('shows the delivery code to tell the rider while out for delivery', async () => {
    const { container } = track(deliveryOrder({ status: 'out_for_delivery' }));
    expect(await screen.findByText('Delivery code')).toBeInTheDocument();
    expect(screen.getByText('Tell this code to the rider when your order arrives.')).toBeInTheDocument();
    expect(screen.getByText('Your order from Raju Kirana is on the way.')).toBeInTheDocument();
    expect(container.textContent).toContain('4821');
  });

  it('calls the shop when the order carries its phone (D-220), and not otherwise', async () => {
    const phone = '+919000000777';
    const view = track(deliveryOrder({ status: 'out_for_delivery', store: { ...deliveryOrder().store, phone } }));
    const call = await screen.findByRole('link', { name: 'Call shop' });
    expect(call.getAttribute('href') === `tel:${phone}`).toBe(true);
    view.unmount();
    track(deliveryOrder({ status: 'out_for_delivery' }));
    expect(await screen.findByText('Delivery code')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Call shop' })).toBeNull();
  });
});

describe('C-18 saved addresses', () => {
  it('lists, makes one the default with If-Match and deletes one', async () => {
    const calls: Call[] = [];
    let list = [address(), far];
    renderAt('/account', {
      returning: true,
      handlers: [
        signedIn,
        on('GET', '/v1/me', () => json(200, { id: USER, name: 'Asha', preferredLang: 'en', phoneMasked: '90•••••011', app: 'customer', createdAt: NOW }, { etag: '"v1"' })),
        on('GET', '/v1/me/addresses', () => json(200, { addresses: list })),
        on('PATCH', `/v1/me/addresses/${FAR}`, (c) => {
          calls.push(c);
          list = [{ ...address(), isDefault: false }, { ...far, isDefault: true, version: 2 }];
          return json(200, list[1]);
        }),
        on('DELETE', `/v1/me/addresses/${HOME}`, (c) => {
          calls.push(c);
          list = list.filter((a) => a.addressId !== HOME);
          return new Response(null, { status: 204 });
        }),
      ],
    });
    const card = await screen.findByRole('region', { name: 'Addresses' });
    expect(await within(card).findByText('4 Lake View, 560038')).toBeInTheDocument();
    expect(within(card).getByText('9 Hebbal Road, 560038')).toBeInTheDocument();

    fireEvent.click(within(card).getByRole('button', { name: 'Make default' }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.headers.get('if-match')).toBe('"v1"');
    expect(calls[0]?.body).toEqual({ isDefault: true });

    fireEvent.click(await within(card).findByRole('button', { name: 'Delete Home' }));
    await waitFor(() => expect(within(card).queryByText('4 Lake View, 560038')).toBeNull());
    expect(calls[1]?.method).toBe('DELETE');
  });
});
