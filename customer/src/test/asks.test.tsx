// W3-16: C-25 ask sheet, C-26 progress and replies, booking from a reply, and the C-08 share link.
import type { Ask } from '@todayz/api-client/gen/model/ask';
import type { AskDetail } from '@todayz/api-client/gen/model/askDetail';
import type { AskReply } from '@todayz/api-client/gen/model/askReply';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { setPlace } from '../app/place';
import { getCart, resetCartForTests } from '../cart/cart';
import { setAskStreamFetchForTests } from '../asks/api';
import { setStreamFetchForTests } from '../orders/stream';
import { isAndroid, whatsappHref } from '../share/links';
import { itemPath, renderShare, shareHtml } from '../../functions/x/[code]';
import { NOW, SHOP_A, item, page, pageOf } from './discover-fixtures';
import { json, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const ASK_ID = '01KE0S1W6W8ZV7T1C3A6ASK001';
const ITEM_ID = '01KE0S1W6W8ZV7T1C3A6ITEM01';
const STORE_ID = '01KE0S1W6W8ZV7T1C3A6MS0001';
const at = (min: number) => new Date(Date.parse(NOW) + min * 60_000).toISOString();

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on = (method: string, path: string, make: (c: Call) => Response): Handler => (c) =>
  c.method === method && c.path === path ? make(c) : undefined;

function ask(over: Partial<Ask> = {}): Ask {
  return {
    askId: ASK_ID,
    itemName: 'Aashirvaad Atta',
    productId: '01KE0S1W6W8ZV7T1C3A6PROD01',
    quantity: 2,
    needBy: 'today',
    status: 'open',
    closeReason: null,
    progress: { asked: 5, replied: 0, bestPricePaise: null },
    yesCount: 0,
    createdAt: NOW,
    closedAt: null,
    windowEndsAt: at(15),
    version: 1,
    ...over,
  } as Ask;
}

function reply(over: Partial<AskReply> = {}): AskReply {
  return {
    askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY01',
    storeId: STORE_ID,
    storeSlug: SHOP_A,
    storeName: 'Sri Lakshmi Stores',
    distanceM: 600,
    pricePaise: 5800,
    readyBy: at(30),
    repliedAt: at(-4),
    priceValidUntil: at(20),
    storeItemId: ITEM_ID,
    listedPricePaise: 6000,
    bookable: true,
    inStoreOnly: false,
    orderId: null,
    ...over,
  };
}

const detail = (a: Ask = ask(), replies: AskReply[] = []): AskDetail => ({ ask: a, replies, serverNow: NOW });

const quietStream = (() =>
  Promise.resolve(new Response(new ReadableStream<Uint8Array>({ start: () => undefined }), { status: 200, headers: { 'content-type': 'text/event-stream' } }))) as typeof fetch;

beforeEach(() => {
  resetCartForTests();
  sessionStorage.clear();
  setAskStreamFetchForTests(quietStream);
  setStreamFetchForTests(quietStream);
});
afterEach(() => {
  setAskStreamFetchForTests(undefined);
  setStreamFetchForTests(undefined);
});

/** The product page (C-08) with the discover answers it needs. */
const productRoutes: Handler = (c) => {
  if (c.path === `/v1/discover/stores/${SHOP_A}`) return json(200, page());
  if (c.path === `/v1/discover/stores/${SHOP_A}/items`) return json(200, pageOf([item()]));
  if (c.path === `/v1/discover/stores/${SHOP_A}/items/${ITEM_ID}`) return json(200, { item: item(), serverNow: NOW });
  if (c.path === '/v1/discover/search') return json(200, { query: 'x', summary: { storesChecked: 0, storesWithIt: 0, minPricePaise: null }, results: [], chips: [], refineHint: null, nextCursor: null, serverNow: NOW });
  return undefined;
};

const atBengaluru = () => act(() => setPlace({ kind: 'gps', lat: 12.97, lng: 77.64 }));

async function openSheet() {
  fireEvent.click(await screen.findByRole('button', { name: 'Ask shops nearby' }));
  return screen.findByRole('dialog');
}

describe('C-25 Ask sheet', () => {
  it('sends the product, quantity, need-by and place once, then opens the ask', async () => {
    const { calls, router } = renderAt(`/s/${SHOP_A}/i/${ITEM_ID}`, {
      returning: true,
      handlers: [signedIn, productRoutes, on('POST', '/v1/me/asks', () => json(201, detail())), on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, detail()))],
    });
    atBengaluru();
    const sheet = await openSheet();
    expect(within(sheet).getByText('Shops see the item, the quantity and roughly how far you are. Never your name or number.')).toBeInTheDocument();
    fireEvent.click(within(sheet).getByRole('button', { name: /One more/ }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Today' }));
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Send' }));

    await waitFor(() => expect(router.state.location.pathname).toBe(`/asks/${ASK_ID}`));
    const post = calls.find((c) => c.method === 'POST' && c.path === '/v1/me/asks');
    expect(post?.body).toEqual({ productId: '01KE0S1W6W8ZV7T1C3A6PROD01', quantity: 2, needBy: 'today', lat: 12.97, lng: 77.64 });
    expect(post?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-Z]{26}$/);
    expect(await screen.findByText('Asked 5 shops · 0 replied')).toBeInTheDocument();
    expect(screen.getByText('Waiting for shops. Most reply within 15 min.')).toBeInTheDocument();
  });

  it('shows the daily limit with the number from the server', async () => {
    renderAt(`/s/${SHOP_A}/i/${ITEM_ID}`, {
      returning: true,
      handlers: [
        signedIn,
        productRoutes,
        on('POST', '/v1/me/asks', () =>
          json(429, { type: 'about:blank', title: 'ask_limit_reached', status: 429, code: 'ask_limit_reached', extensions: { limit: 3 } }, { 'Retry-After': '3600' }),
        ),
      ],
    });
    atBengaluru();
    const sheet = await openSheet();
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Send' }));
    expect(await within(sheet).findByRole('alert')).toHaveTextContent('You can send 3 asks a day. Try again tomorrow.');
  });

  it('links to the open ask when one is already open', async () => {
    renderAt(`/s/${SHOP_A}/i/${ITEM_ID}`, {
      returning: true,
      handlers: [
        signedIn,
        productRoutes,
        on('POST', '/v1/me/asks', () => json(409, { type: 'about:blank', title: 'x', status: 409, code: 'ask_already_open', extensions: { askId: ASK_ID } })),
      ],
    });
    atBengaluru();
    const sheet = await openSheet();
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Send' }));
    expect(await within(sheet).findByRole('link', { name: 'See it' })).toHaveAttribute('href', `/asks/${ASK_ID}`);
  });

  it('signed out: keeps the fields and goes to sign-in, then reopens the sheet with them', async () => {
    const { router, calls } = renderAt(`/s/${SHOP_A}/i/${ITEM_ID}`, { handlers: [productRoutes] });
    atBengaluru();
    const sheet = await openSheet();
    fireEvent.click(within(sheet).getByRole('button', { name: /One more/ }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Tomorrow' }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Sign in to send' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
    expect(router.state.location.search).toEqual({ next: `/s/${SHOP_A}/i/${ITEM_ID}` });
    expect(calls.some((c) => c.path === '/v1/me/asks')).toBe(false);

    // Back on the page (as after C-10): the sheet is open with quantity 2 and Tomorrow pressed.
    await act(() => router.navigate({ to: '/s/$slug/i/$itemId', params: { slug: SHOP_A, itemId: ITEM_ID } }));
    const again = await screen.findByRole('dialog');
    expect(within(again).getByRole('button', { name: 'Tomorrow' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(again).getByText('2')).toBeInTheDocument();
  });
});

describe('C-26 Ask progress and replies', () => {
  const replies = [
    reply(),
    reply({ askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY02', storeName: 'Annapurna Kirana', storeSlug: 'annapurna-kirana', storeId: '01KE0S1W6W8ZV7T1C3A6MS0002', pricePaise: null, priceValidUntil: null, readyBy: at(10), orderId: '01KE0S1W6W8ZV7T1C3A6ORDR01' }),
    reply({ askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY03', storeName: 'Corner Shop', storeId: '01KE0S1W6W8ZV7T1C3A6MS0003', readyBy: null, inStoreOnly: true, bookable: false }),
  ];
  const withReplies = detail(ask({ progress: { asked: 5, replied: 4, bestPricePaise: 5800 }, yesCount: 3 }), replies);

  it('shows progress, the soonest badge, prices and the right action on each card', async () => {
    renderAt(`/asks/${ASK_ID}`, { returning: true, handlers: [signedIn, on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, withReplies))] });
    expect(await screen.findByText('Asked 5 shops · 4 replied · best ₹58')).toBeInTheDocument();
    const cards = screen.getAllByRole('article');
    expect(cards).toHaveLength(3);
    expect(within(cards[0] as HTMLElement).getByText('₹58')).toBeInTheDocument();
    expect(within(cards[0] as HTMLElement).getByRole('button', { name: 'Book' })).toBeInTheDocument();
    expect(within(cards[1] as HTMLElement).getByText('Ready soonest')).toBeInTheDocument();
    expect(within(cards[1] as HTMLElement).getByText('Ask the shop for the price')).toBeInTheDocument();
    expect(within(cards[1] as HTMLElement).getByRole('link', { name: 'View order' })).toHaveAttribute('href', '/orders/01KE0S1W6W8ZV7T1C3A6ORDR01');
    expect(within(cards[2] as HTMLElement).getByText('In-store only: visit the shop')).toBeInTheDocument();
    expect(within(cards[2] as HTMLElement).queryByRole('button', { name: 'Book' })).toBeNull();
  });

  it('Book puts the asked quantity and the reply in the cart and opens checkout', async () => {
    const { router } = renderAt(`/asks/${ASK_ID}`, { returning: true, handlers: [signedIn, on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, withReplies))] });
    fireEvent.click(await screen.findByRole('button', { name: 'Book' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/checkout'));
    const cart = getCart();
    expect(cart.store?.id).toBe(STORE_ID);
    expect(cart.lines).toEqual([expect.objectContaining({ storeItemId: ITEM_ID, qty: 2, pricePaise: 5800, askReplyId: '01KE0S1W6W8ZV7T1C3A6RPLY01' })]);
  });

  it('once the replied price ends: "Price may have changed" and the current price', async () => {
    const expired = detail(ask({ progress: { asked: 5, replied: 1, bestPricePaise: 5800 } }), [reply({ priceValidUntil: at(-1) })]);
    renderAt(`/asks/${ASK_ID}`, { returning: true, handlers: [signedIn, on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, expired))] });
    expect(await screen.findByText(/Price may have changed · ₹60/)).toBeInTheDocument();
  });

  it('closed with no Yes says so; signed out asks to sign in', async () => {
    renderAt(`/asks/${ASK_ID}`, {
      returning: true,
      handlers: [signedIn, on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, detail(ask({ status: 'closed', closeReason: 'window_ended', windowEndsAt: null }))))],
    });
    expect(await screen.findByText("No shop has it right now. We've told shops nearby people want it.")).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Close ask' })).toBeNull();
  });

  it('Close ask closes it', async () => {
    const { calls } = renderAt(`/asks/${ASK_ID}`, {
      returning: true,
      handlers: [
        signedIn,
        on('GET', `/v1/me/asks/${ASK_ID}`, () => json(200, detail())),
        on('POST', `/v1/me/asks/${ASK_ID}/close`, () => json(200, detail(ask({ status: 'closed', closeReason: 'customer_closed', windowEndsAt: null })))),
      ],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Close ask' }));
    expect(await screen.findByText('Closed')).toBeInTheDocument();
    expect(calls.filter((c) => c.path === `/v1/me/asks/${ASK_ID}/close`)).toHaveLength(1);
  });

  it('the list shows each ask with its progress and links to it', async () => {
    renderAt('/asks', {
      returning: true,
      handlers: [signedIn, on('GET', '/v1/me/asks', () => json(200, { asks: [ask({ progress: { asked: 5, replied: 2, bestPricePaise: 5800 }, yesCount: 2 })], nextCursor: null }))],
    });
    const link = await screen.findByRole('link', { name: /Aashirvaad Atta/ });
    expect(link).toHaveAttribute('href', `/asks/${ASK_ID}`);
    expect(within(link).getByText('Asked 5 shops · 2 replied · best ₹58')).toBeInTheDocument();
    expect(within(link).getByText('2 shops have it')).toBeInTheDocument();
  });

  it('signed out, the page asks to sign in', async () => {
    renderAt(`/asks/${ASK_ID}`);
    expect(await screen.findByText('Sign in to see your asks')).toBeInTheDocument();
  });
});

describe('Share link /x/{code}', () => {
  const CODE = 'aB3dE5gH7j';
  const preview = {
    storeItemId: ITEM_ID,
    productId: null,
    itemName: 'Amul Butter 500 g',
    storeName: 'Sharma Kirana',
    storeSlug: 'sharma-kirana',
    locality: 'Koramangala',
    pricePaise: 3800,
    mrpPaise: 4000,
    imageUrl: 'https://img.todayz.in/share/aB3dE5gH7j.png',
  };

  it('opens C-08 with ref=share', async () => {
    const { router } = renderAt(`/x/${CODE}`, { handlers: [on('GET', `/v1/public/share-cards/${CODE}`, () => json(200, preview))] });
    await waitFor(() => expect(router.state.location.pathname).toBe(`/s/sharma-kirana/i/${ITEM_ID}`));
    expect(router.state.location.search).toMatchObject({ ref: 'share' });
  });

  it('a shop that is no longer live shows the notice and a search box', async () => {
    const { router } = renderAt(`/x/${CODE}`, {
      handlers: [on('GET', `/v1/public/share-cards/${CODE}`, () => json(410, { type: 'about:blank', title: 'x', status: 410, code: 'share_card_unavailable' }))],
    });
    expect(await screen.findByText("This shop isn't on TodayZ right now")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Search for an item'), { target: { value: 'butter' } });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(router.state.location.pathname).toBe('/search'));
    expect(router.state.location.search).toMatchObject({ q: 'butter' });
  });

  it('the Pages Function renders the preview without a distance and falls through on failure', async () => {
    const ok = (() => Promise.resolve(json(200, preview))) as typeof fetch;
    const r = await renderShare(CODE, 'https://app.todayz.in', 'https://api.todayz.in', ok);
    const html = (await r?.text()) ?? '';
    expect(html).toContain('<meta property="og:title" content="Amul Butter 500 g at Sharma Kirana">');
    expect(html).toContain('₹38 · Koramangala · TodayZ');
    expect(html).toContain(itemPath(preview));
    expect(html).not.toMatch(/\bkm\b|\bm away\b/);
    const gone = (() => Promise.resolve(json(410, { code: 'share_card_unavailable' }))) as typeof fetch;
    expect(await renderShare(CODE, 'https://app.todayz.in', 'https://api.todayz.in', gone)).toBeNull();
    expect(await renderShare('bad', 'https://app.todayz.in', 'https://api.todayz.in', ok)).toBeNull();
    expect(shareHtml({ ...preview, itemName: '<b>"x"</b>' }, 'https://app.todayz.in', CODE)).not.toContain('<b>');
  });

  it('WhatsApp first only on Android, with the text and the link', () => {
    expect(isAndroid('Mozilla/5.0 (Linux; Android 14; Pixel 8)')).toBe(true);
    expect(isAndroid('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)')).toBe(false);
    expect(whatsappHref({ text: 'Amul Butter at Sharma Kirana, ₹38 · TodayZ', url: 'https://app.todayz.in/x/aB3dE5gH7j' })).toBe(
      `https://wa.me/?text=${encodeURIComponent('Amul Butter at Sharma Kirana, ₹38 · TodayZ https://app.todayz.in/x/aB3dE5gH7j')}`,
    );
  });

  it('C-08 Share gets the card and offers Copy link where there is no system sheet', async () => {
    const { calls } = renderAt(`/s/${SHOP_A}/i/${ITEM_ID}`, {
      handlers: [productRoutes, on('POST', '/v1/public/share-cards', () => json(200, { code: CODE, url: `https://app.todayz.in/x/${CODE}`, text: 'Aashirvaad Atta at Sri Lakshmi Stores, ₹260 · TodayZ', imageUrl: preview.imageUrl }))],
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Share' }));
    const sheet = await screen.findByRole('dialog');
    expect(within(sheet).getByRole('button', { name: 'Copy link' })).toBeInTheDocument();
    expect(within(sheet).queryByRole('link', { name: 'WhatsApp' })).toBeNull();
    const post = calls.find((c) => c.path === '/v1/public/share-cards');
    expect(post?.body).toMatchObject({ storeItemId: ITEM_ID });
  });
});
