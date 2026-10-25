// W3-12 consumer AI on C-05 and C-22: voice search (C-23, AI-C5), search assist (AI-C2) and list
// parse (AI-C1). Each one shows only when GET /v1/me/ai/features says so for a signed-in customer;
// nothing applies, searches or joins a basket until the customer does it.
import type { AiCustomerFeatures } from '@todayz/api-client/gen/model/aiCustomerFeatures';
import type { Basket } from '@todayz/api-client/gen/model/basket';
import type { ListParseDraft } from '@todayz/api-client/gen/model/listParseDraft';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setAiEventSink } from '../ai/events';
import { clearCart } from '../cart/cart';
import { resetVoiceSearchForTests } from '../voice-search/session';
import { CATEGORIES, NOW, atBengaluru, card, item } from './discover-fixtures';
import { json, problem, renderAt, type Call, type Handler } from './render';

const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const BASKET_ID = '01KE0MK0000000000000000001';
const DRAFT_ID = '01KE0LP0000000000000000001';

const grant = () => ({ accessToken: 'tok', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: USER });
const signedIn: Handler = (c) => (c.path === '/v1/auth/refresh' ? json(200, grant()) : undefined);
const on =
  (method: string, path: string, make: (c: Call) => Response): Handler =>
  (c) =>
    c.method === method && c.path === path ? make(c) : undefined;

const features = (over: Partial<AiCustomerFeatures> = {}): AiCustomerFeatures => ({
  listParse: true,
  voiceSearch: true,
  searchAssist: true,
  maxAudioSeconds: 15,
  maxAudioBytes: 1_000_000,
  audioTypes: ['audio/webm', 'audio/ogg', 'audio/mp4'],
  ...over,
});
const featuresOn = (f: AiCustomerFeatures = features()): Handler => on('GET', '/v1/me/ai/features', () => json(200, f));

const result = (over: Record<string, unknown> = {}) => ({
  query: 'atta',
  summary: { storesChecked: 12, storesWithIt: 1, minPricePaise: 24000 },
  results: [{ store: card(), items: [item()], badge: 'lowest_price', deliveryFeePaise: 2000 }],
  chips: [],
  refineHint: false,
  nextCursor: null,
  serverNow: NOW,
  ...over,
});
const discover = (r: () => unknown = () => result()): Handler[] => [
  on('GET', '/v1/discover/search', () => json(200, r())),
  on('GET', '/v1/discover/search/suggest', () => json(200, { items: [] })),
  on('GET', '/v1/discover/categories', () => json(200, CATEGORIES)),
];

// --- A browser that can record: MediaRecorder and getUserMedia, with a clock the test moves. ---

let clock = 0;
let tracksStopped = 0;
let micRefused = false;

class FakeRecorder {
  static isTypeSupported = (t: string) => t.startsWith('audio/webm');
  state: 'inactive' | 'recording' = 'inactive';
  mimeType: string;
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  constructor(_stream: unknown, opts: { mimeType: string }) {
    this.mimeType = opts.mimeType;
  }
  start() {
    this.state = 'recording';
  }
  stop() {
    this.state = 'inactive';
    queueMicrotask(() => {
      this.ondataavailable?.({ data: new Blob(['opus-bytes'], { type: this.mimeType }) });
      this.onstop?.();
    });
  }
}

function canRecord(): void {
  vi.stubGlobal('MediaRecorder', FakeRecorder);
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      getUserMedia: () =>
        micRefused
          ? Promise.reject(new DOMException('no', 'NotAllowedError'))
          : Promise.resolve({ getTracks: () => [{ stop: () => (tracksStopped += 1) }] }),
    },
  });
  vi.spyOn(performance, 'now').mockImplementation(() => clock);
}

const events: { name: string; props: unknown }[] = [];

beforeEach(() => {
  clearCart();
  resetVoiceSearchForTests();
  clock = 0;
  tracksStopped = 0;
  micRefused = false;
  events.length = 0;
  setAiEventSink((name, props) => events.push({ name, props }));
});

afterEach(() => {
  setAiEventSink(null);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(navigator, 'mediaDevices');
});

async function speak(): Promise<void> {
  fireEvent.click(await screen.findByRole('button', { name: 'Search by voice' }));
  const stop = await screen.findByRole('button', { name: 'Stop and use what I said' });
  expect(stop).toHaveAttribute('aria-pressed', 'true');
  clock += 2500;
  fireEvent.click(stop);
}

describe('C-23 voice search', () => {
  it('shows no mic when signed out, and never asks for the features', async () => {
    canRecord();
    const { calls } = renderAt('/search', { handlers: [featuresOn(), ...discover()] });
    atBengaluru();
    expect(await screen.findByRole('searchbox')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Search by voice' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/me/ai/features')).toBe(false);
  });

  it('shows no mic when the switch is off, the browser cannot record, or the device is offline', async () => {
    canRecord();
    renderAt('/search', { returning: true, handlers: [signedIn, featuresOn(features({ voiceSearch: false })), ...discover()] });
    expect(await screen.findByRole('searchbox')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Search by voice' })).not.toBeInTheDocument());

    // No container the server takes.
    renderAt('/search', { returning: true, handlers: [signedIn, featuresOn(features({ audioTypes: ['audio/wav'] })), ...discover()] });
    expect(await screen.findAllByRole('searchbox')).not.toHaveLength(0);
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Search by voice' })).not.toBeInTheDocument());
  });

  it('hides the mic while offline', async () => {
    canRecord();
    renderAt('/search', { returning: true, handlers: [signedIn, featuresOn(), ...discover()] });
    expect(await screen.findByRole('button', { name: 'Search by voice' })).toBeInTheDocument();
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
      window.dispatchEvent(new Event('offline'));
    });
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Search by voice' })).not.toBeInTheDocument());
    act(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: true });
      window.dispatchEvent(new Event('online'));
    });
  });

  it('fills the box with the transcript without searching; the search then says source=voice', async () => {
    canRecord();
    const uploads: Call[] = [];
    const { calls } = renderAt('/search', {
      returning: true,
      handlers: [
        signedIn,
        featuresOn(),
        on('POST', '/v1/me/ai/voice-search', (c) => {
          uploads.push(c);
          return json(200, { transcript: 'do kilo atta', language: 'hi', chips: [] });
        }),
        ...discover(),
      ],
    });
    atBengaluru();
    await speak();
    const box = await screen.findByRole('searchbox');
    await waitFor(() => expect(box).toHaveValue('do kilo atta'));
    expect(screen.getByText('Check the words, then search.')).toBeInTheDocument();
    expect(tracksStopped).toBe(1);
    // The upload is the recording as multipart, in the type the server takes.
    const form = uploads[0]?.body as FormData;
    const audio = form.get('audio') as Blob;
    expect(audio.type).toBe('audio/webm');
    expect(audio.size).toBeGreaterThan(0);
    // Nothing searched by itself.
    expect(calls.some((c) => c.path === '/v1/discover/search')).toBe(false);

    fireEvent.submit(box.closest('form') as HTMLFormElement);
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/discover/search')).toBe(true));
    const search = calls.find((c) => c.path === '/v1/discover/search');
    expect(search?.query.get('q')).toBe('do kilo atta');
    expect(search?.query.get('source')).toBe('voice');
    expect(events).toContainEqual({ name: 'voice_search_used', props: { lang: 'hi', seconds: 3, outcome: 'searched' } });
  });

  it('counts an edited transcript as edited_then_searched', async () => {
    canRecord();
    renderAt('/search', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/voice-search', () => json(200, { transcript: 'atta', language: 'en', chips: [] })), ...discover()],
    });
    atBengaluru();
    await speak();
    const box = await screen.findByRole('searchbox');
    await waitFor(() => expect(box).toHaveValue('atta'));
    fireEvent.change(box, { target: { value: 'atta 5 kg' } });
    fireEvent.submit(box.closest('form') as HTMLFormElement);
    await waitFor(() => expect(events.map((e) => (e.props as { outcome: string }).outcome)).toContain('edited_then_searched'));
  });

  it('at the cap: the mic is disabled with the C-23 copy', async () => {
    canRecord();
    renderAt('/search', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/voice-search', () => problem(429, 'ai_daily_cap_reached')), ...discover()],
    });
    await speak();
    const capped = await screen.findByRole('button', { name: 'Voice search is used up for today. Type instead.' });
    expect(capped).toBeDisabled();
    expect(screen.getAllByRole('status').some((el) => el.textContent === 'Voice search is used up for today. Type instead.')).toBe(true);
    expect(events).toContainEqual({ name: 'voice_search_used', props: { lang: 'en', seconds: 3, outcome: 'cap_reached' } });
  });

  it('a provider failure hides the mic for the session, with the C-23 copy', async () => {
    canRecord();
    renderAt('/search', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/voice-search', () => problem(503, 'ai_unavailable')), ...discover()],
    });
    await speak();
    expect(await screen.findByText("Voice search isn't working right now. Type instead.")).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Search by voice' })).not.toBeInTheDocument();
    expect(screen.getByRole('searchbox')).toHaveValue('');
  });

  it('a refused microphone says so and keeps the box', async () => {
    canRecord();
    micRefused = true;
    renderAt('/search', { returning: true, handlers: [signedIn, featuresOn(), ...discover()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Search by voice' }));
    expect(await screen.findByText('Allow the microphone to search by voice, or type instead.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Search by voice' })).toBeEnabled();
  });
});

describe('AI-C2 search assist', () => {
  const assistAnswer = () =>
    json(200, {
      query: 'atta',
      chips: [
        { kind: 'maxPrice', value: '30000', text: 'under 300' },
        { kind: 'openNow', value: null, text: 'open now' },
        { kind: 'brand', value: 'aashirvaad', text: 'Aashirvaad' },
      ],
    });

  it('is not asked when the response has no refineHint', async () => {
    const { calls } = renderAt('/search?q=atta', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/search-assist', assistAnswer), ...discover()],
    });
    atBengaluru();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    await act(() => new Promise((r) => setTimeout(r, 20)));
    expect(calls.some((c) => c.path === '/v1/me/ai/search-assist')).toBe(false);
  });

  it('is not asked when signed out, even with refineHint', async () => {
    const { calls } = renderAt('/search?q=atta', {
      handlers: [featuresOn(), on('POST', '/v1/me/ai/search-assist', assistAnswer), ...discover(() => result({ refineHint: true }))],
    });
    atBengaluru();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    await act(() => new Promise((r) => setTimeout(r, 20)));
    expect(calls.some((c) => c.path === '/v1/me/ai/search-assist')).toBe(false);
  });

  it('after the results, shows chips that apply only when tapped', async () => {
    const asks: Call[] = [];
    const { calls, router } = renderAt('/search?q=aata%20under%20300%20open', {
      returning: true,
      handlers: [
        signedIn,
        featuresOn(),
        on('POST', '/v1/me/ai/search-assist', (c) => {
          asks.push(c);
          return assistAnswer();
        }),
        ...discover(() => result({ query: 'aata under 300 open', refineHint: true })),
      ],
    });
    atBengaluru();
    const group = await screen.findByRole('group', { name: 'Suggestions for this search' });
    expect(asks[0]?.body).toEqual({ q: 'aata under 300 open' });
    // Results came first.
    expect(calls.findIndex((c) => c.path === '/v1/discover/search')).toBeLessThan(calls.findIndex((c) => c.path === '/v1/me/ai/search-assist'));
    expect(within(group).getByRole('button', { name: 'Search for “atta”' })).toBeInTheDocument();
    expect(within(group).getByRole('button', { name: 'Open now' })).toBeInTheDocument();
    expect(within(group).getByRole('button', { name: 'Aashirvaad' })).toBeInTheDocument();
    const searches = () => calls.filter((c) => c.path === '/v1/discover/search');
    const before = searches().length;

    fireEvent.click(within(group).getByRole('button', { name: /Under ₹300/ }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ maxPricePaise: 30000 }));
    await waitFor(() => expect(searches().length).toBeGreaterThan(before));
    expect(searches().at(-1)?.query.get('maxPricePaise')).toBe('30000');
    expect(searches().at(-1)?.query.get('parse')).toBe('false');
    const applied = await screen.findByRole('group', { name: 'Added filters' });
    expect(within(applied).getByRole('button', { name: /Under ₹300/ })).toBeInTheDocument();
    expect(events.map((e) => (e.props as { outcome: string }).outcome)).toEqual(['shown', 'tapped']);
  });

  it('shows nothing on a 204, and the results stay', async () => {
    renderAt('/search?q=atta', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/search-assist', () => new Response(null, { status: 204 })), ...discover(() => result({ refineHint: true }))],
    });
    atBengaluru();
    expect(await screen.findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    await waitFor(() => expect(events).toHaveLength(1));
    expect(events[0]).toMatchObject({ name: 'search_assist_used', props: { outcome: 'empty' } });
    expect(screen.queryByRole('group', { name: 'Suggestions for this search' })).not.toBeInTheDocument();
  });

  it('shows nothing at the cap', async () => {
    renderAt('/search?q=atta', {
      returning: true,
      handlers: [signedIn, featuresOn(), on('POST', '/v1/me/ai/search-assist', () => problem(429, 'ai_daily_cap_reached')), ...discover(() => result({ refineHint: true }))],
    });
    atBengaluru();
    await waitFor(() => expect(events[0]).toMatchObject({ props: { outcome: 'cap_reached' } }));
    expect(screen.queryByRole('group', { name: 'Suggestions for this search' })).not.toBeInTheDocument();
    expect(screen.getByText('Sri Lakshmi Stores')).toBeInTheDocument();
  });
});

describe('AI-C1 list parse on C-22', () => {
  const basket = (): Basket =>
    ({
      basketId: BASKET_ID,
      name: 'Weekly',
      lines: [],
      lastSplit: null,
      updatedAt: NOW,
      version: 1,
    }) as unknown as Basket;
  const draft = (over: Partial<ListParseDraft> = {}): ListParseDraft => ({
    draftId: DRAFT_ID,
    feature: 'list_parse',
    status: 'ready',
    promptVersion: 'list_parse@1',
    createdAt: NOW,
    outcomes: [],
    lines: [
      { lineId: 'l1', text: 'atta 1 kg', quantity: 1 },
      { lineId: 'l2', text: 'nandini milk', quantity: 2 },
      { lineId: 'l3', text: 'dolo 650', quantity: 1 },
    ],
    failure: null,
    ...over,
  });

  function renderBasket(extra: Handler[], f: AiCustomerFeatures = features()) {
    return renderAt(`/baskets/${BASKET_ID}`, {
      returning: true,
      handlers: [
        signedIn,
        featuresOn(f),
        on('GET', `/v1/me/baskets/${BASKET_ID}`, () => json(200, basket())),
        on('PATCH', `/v1/me/baskets/${BASKET_ID}`, () => json(200, basket())),
        ...extra,
      ],
    });
  }

  it('is not offered when the switch is off', async () => {
    renderBasket([], features({ listParse: false }));
    expect(await screen.findByRole('heading', { name: 'Weekly' })).toBeInTheDocument();
    await act(() => new Promise((r) => setTimeout(r, 20)));
    expect(screen.queryByRole('button', { name: 'Paste a list' })).not.toBeInTheDocument();
  });

  it('reads a list into draft lines; only the kept and edited lines join the basket', async () => {
    const creates: Call[] = [];
    const confirms: Call[] = [];
    let polls = 0;
    renderBasket([
      on('POST', '/v1/me/ai/list-parse/drafts', (c) => {
        creates.push(c);
        return json(202, { draftId: DRAFT_ID });
      }),
      on('GET', `/v1/me/ai/list-parse/drafts/${DRAFT_ID}`, () => {
        polls += 1;
        return json(200, polls === 1 ? draft({ status: 'processing', lines: [] }) : draft());
      }),
      on('POST', `/v1/me/ai/list-parse/drafts/${DRAFT_ID}/confirm`, (c) => {
        confirms.push(c);
        return json(200, {
          draftId: DRAFT_ID,
          status: 'confirmed',
          lines: draft().lines,
          basketLines: [
            { lineId: 'l1', text: 'atta 1 kg', quantity: 1 },
            { lineId: 'l2', text: 'nandini milk 500 ml', quantity: 2 },
          ],
        });
      }),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: 'Paste a list' }));
    fireEvent.change(screen.getByLabelText('Your list'), { target: { value: 'atta 1 kg, 2 nandini milk, dolo 650' } });
    fireEvent.click(screen.getByRole('button', { name: 'Read my list' }));

    expect(await screen.findByText('Reading your list…')).toBeInTheDocument();
    expect(creates[0]?.body).toEqual({ text: 'atta 1 kg, 2 nandini milk, dolo 650' });
    expect(creates[0]?.headers.get('Idempotency-Key')).toBeTruthy();

    const review = await screen.findByRole('region', { name: 'Check your list' }, { timeout: 3000 });
    // Nothing joined the basket yet.
    expect(screen.queryByRole('list', { name: 'Items in this basket' })).not.toBeInTheDocument();
    fireEvent.change(within(review).getByRole('textbox', { name: 'Item 2' }), { target: { value: 'nandini milk 500 ml' } });
    fireEvent.click(within(review).getByRole('button', { name: 'Remove dolo 650' }));
    expect(within(review).getByRole('button', { name: 'Keep' })).toBeInTheDocument();
    fireEvent.click(within(review).getByRole('button', { name: 'Add 2 items to basket' }));

    await waitFor(() => expect(confirms).toHaveLength(1));
    expect(confirms[0]?.body).toEqual({
      lines: [
        { lineId: 'l1', outcome: 'accepted' },
        { lineId: 'l2', outcome: 'edited', value: { text: 'nandini milk 500 ml', quantity: 2 } },
        { lineId: 'l3', outcome: 'discarded' },
      ],
    });
    const list = await screen.findByRole('list', { name: 'Items in this basket' });
    expect(within(list).getByText('atta 1 kg')).toBeInTheDocument();
    expect(within(list).getByText('nandini milk 500 ml')).toBeInTheDocument();
    expect(within(list).queryByText('dolo 650')).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Check your list' })).not.toBeInTheDocument();
    expect(events).toContainEqual({ name: 'list_parse_used', props: { outcome: 'confirmed', lines: 2 } });
  });

  it('discarding sends every line as discarded and adds nothing', async () => {
    const confirms: Call[] = [];
    renderBasket([
      on('POST', '/v1/me/ai/list-parse/drafts', () => json(202, { draftId: DRAFT_ID })),
      on('GET', `/v1/me/ai/list-parse/drafts/${DRAFT_ID}`, () => json(200, draft())),
      on('POST', `/v1/me/ai/list-parse/drafts/${DRAFT_ID}/confirm`, (c) => {
        confirms.push(c);
        return json(200, { draftId: DRAFT_ID, status: 'discarded', lines: draft().lines, basketLines: [] });
      }),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: 'Paste a list' }));
    fireEvent.change(screen.getByLabelText('Your list'), { target: { value: 'atta, milk, dolo' } });
    fireEvent.click(screen.getByRole('button', { name: 'Read my list' }));
    const review = await screen.findByRole('region', { name: 'Check your list' });
    fireEvent.click(within(review).getByRole('button', { name: 'Discard list' }));
    await waitFor(() => expect(confirms).toHaveLength(1));
    expect((confirms[0]?.body as { lines: { outcome: string }[] }).lines.every((l) => l.outcome === 'discarded')).toBe(true);
    expect(await screen.findByRole('button', { name: 'Paste a list' })).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Items in this basket' })).not.toBeInTheDocument();
  });

  it('at the cap says so, and the basket is untouched', async () => {
    renderBasket([on('POST', '/v1/me/ai/list-parse/drafts', () => problem(429, 'ai_daily_cap_reached'))]);
    fireEvent.click(await screen.findByRole('button', { name: 'Paste a list' }));
    fireEvent.change(screen.getByLabelText('Your list'), { target: { value: 'atta' } });
    fireEvent.click(screen.getByRole('button', { name: 'Read my list' }));
    expect(await screen.findByText('Reading lists is used up for today. Add the items one by one.')).toBeInTheDocument();
    expect(events).toContainEqual({ name: 'list_parse_used', props: { outcome: 'cap_reached', lines: 0 } });
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(await screen.findByRole('button', { name: 'Paste a list' })).toBeInTheDocument();
  });

  it('a failed draft offers to try again', async () => {
    renderBasket([
      on('POST', '/v1/me/ai/list-parse/drafts', () => json(202, { draftId: DRAFT_ID })),
      on('GET', `/v1/me/ai/list-parse/drafts/${DRAFT_ID}`, () => json(200, draft({ status: 'failed', lines: [], failure: 'unreadable' }))),
    ]);
    fireEvent.click(await screen.findByRole('button', { name: 'Paste a list' }));
    fireEvent.change(screen.getByLabelText('Your list'), { target: { value: '???' } });
    fireEvent.click(screen.getByRole('button', { name: 'Read my list' }));
    expect(await screen.findByText(/We couldn't read that list/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByLabelText('Your list')).toHaveValue('???');
  });
});
