import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const BASE = '/v1/biz/businesses/b1/stores/s1';
const VOICE = `${BASE}/ai/voice-stock`;

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], nextCursor: null })
      : undefined;
}

const AVAILABLE = { maxSeconds: 15, maxBytes: 1_048_576, languages: ['en-IN', 'hi-IN', 'kn-IN', 'te-IN', 'ta-IN'] };

function line(id: string, over: Record<string, unknown> = {}) {
  return {
    lineId: id,
    kind: 'sale',
    storeItemId: `i-${id}`,
    itemName: `Item ${id}`,
    heard: `sold 2 item ${id}`,
    quantity: 2,
    pricePaise: null,
    currentPricePaise: 5800,
    onHand: 10,
    confidence: 0.92,
    matched: true,
    disabled: null,
    ...over,
  };
}

function draft(status: string, lines: ReturnType<typeof line>[] = [], over: Record<string, unknown> = {}) {
  return {
    draftId: 'd1',
    feature: 'voice_stock',
    status,
    promptVersion: 'voice_stock.v1',
    createdAt: '2026-10-25T08:00:00.000Z',
    language: status === 'processing' ? null : 'en-IN',
    transcript: status === 'processing' ? null : 'sold 2 maggi',
    lines,
    outcomes: [],
    failure: null,
    ...over,
  };
}

/** Availability, the upload (202), and the draft: `processing` on the first read, then `then`. */
function voiceHandlers(then: ReturnType<typeof draft>): Handler[] {
  let reads = 0;
  return [
    (c) => (c.method === 'GET' && c.path === VOICE ? json(200, AVAILABLE) : undefined),
    (c) => (c.method === 'POST' && c.path === `${VOICE}/drafts` ? json(202, { draftId: 'd1', status: 'processing' }) : undefined),
    (c) => {
      if (c.method !== 'GET' || c.path !== `${VOICE}/drafts/d1`) return undefined;
      reads++;
      return json(200, reads === 1 ? draft('processing') : then);
    },
  ];
}

/** A draft a cashier has confirmed in part: the sale applied, the restock waiting for a manager. */
const PARTIAL = '01JB2Q7Z8M3V6Y4K2T9W5R1C0D';
function partialHandlers(role: 'cashier' | 'manager'): Handler[] {
  // The server flags a line ask_manager only for a viewer who may not confirm its kind.
  const disabled = role === 'cashier' ? 'ask_manager' : null;
  const d = draft('ready', [line('a'), line('b', { kind: 'restock', quantity: 10, disabled, heard: '10 milk came' })], {
    draftId: PARTIAL,
    outcomes: [
      { lineId: 'a', outcome: 'accepted' },
      { lineId: 'b', outcome: 'not_permitted' },
    ],
  });
  return [
    (c) => (c.method === 'GET' && c.path === VOICE ? json(200, AVAILABLE) : undefined),
    (c) => (c.method === 'GET' && c.path === `${VOICE}/drafts/${PARTIAL}` ? json(200, d) : undefined),
  ];
}

/** A MediaRecorder that hands back one small chunk on stop; jsdom has none. */
class FakeRecorder {
  static isTypeSupported(t: string) {
    return t === 'audio/webm';
  }
  static size = 2048;
  state: 'inactive' | 'recording' = 'inactive';
  mimeType = 'audio/webm';
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  start() {
    this.state = 'recording';
  }
  stop() {
    this.state = 'inactive';
    this.ondataavailable?.({ data: new Blob([new Uint8Array(FakeRecorder.size)], { type: 'audio/webm' }) });
    this.onstop?.();
  }
}

let clock = 0;
beforeEach(() => {
  clock = 0;
  FakeRecorder.size = 2048;
  vi.stubGlobal('MediaRecorder', FakeRecorder);
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: () => Promise.resolve({ getTracks: () => [{ stop: () => undefined }] }) },
  });
  // Each read of the clock is 0.6 s later, so a hold is long enough to count.
  vi.spyOn(performance, 'now').mockImplementation(() => (clock += 600));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** Hold Space on the talk button, wait until it listens, then let go. */
async function speak() {
  const btn = await screen.findByRole('button', { name: 'Hold to talk' });
  fireEvent.keyDown(btn, { key: ' ' });
  const live = await screen.findByRole('button', { name: 'Let go to send' });
  await act(async () => {
    fireEvent.keyUp(live, { key: ' ' });
    await Promise.resolve();
  });
}

describe('SC-VO-07 voice update is hidden while off', () => {
  it('shows the off state on /voice when the API answers 404, and no way in from Stock', async () => {
    renderAt('/voice', { messages, handlers: [shops('owner')] });
    expect(await screen.findByText('Voice update is off')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Hold to talk' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open stock' })).toHaveAttribute('href', '/stock');
  });

  it('puts a Voice update link on Stock while voice is on', async () => {
    renderAt('/stock', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'GET' && c.path === VOICE ? json(200, AVAILABLE) : undefined),
        (c) => (c.method === 'GET' && c.path === `${BASE}/items` ? json(200, { items: [], nextCursor: null }) : undefined),
      ],
    });
    expect(await screen.findByRole('link', { name: 'Voice update' })).toHaveAttribute('href', '/voice');
  });
});

describe('SC-VO-01 recording to a draft, nothing written before Confirm', () => {
  it('offers the languages, sends the recording, shows processing, then the draft lines', async () => {
    const { calls } = renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('ready', [line('a')]))] });
    const langs = await screen.findByRole('group', { name: 'Language you speak' });
    expect(within(langs).getAllByRole('button')).toHaveLength(6);
    expect(within(langs).getByRole('button', { name: 'Detect' })).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(within(langs).getByRole('button', { name: 'ಕನ್ನಡ' }));
    expect(within(langs).getByRole('button', { name: 'ಕನ್ನಡ' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('Speak in this language for the best match.')).toBeInTheDocument();
    expect(screen.getByText('The recording is used once to make the draft and is not kept.')).toBeInTheDocument();

    await speak();
    expect(await screen.findByText('Working out what you said')).toBeInTheDocument();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    expect(within(list).getByText('Item a')).toBeInTheDocument();
    expect(within(list).getByText('Sold 2', { selector: 'p' })).toBeInTheDocument();
    expect(within(list).getByText('Sure 92%', { exact: false })).toBeInTheDocument();
    expect(screen.getByText('sold 2 maggi')).toBeInTheDocument();

    expect(calls.filter((c) => c.method === 'POST' && c.path === `${VOICE}/drafts`)).toHaveLength(1);
    expect(calls.some((c) => c.path.endsWith('/confirm'))).toBe(false);
    expect(calls.some((c) => c.method !== 'GET' && !c.path.startsWith(VOICE) && c.path !== '/v1/auth/refresh')).toBe(false);
  });
});

describe('SC-VO-03 a cashier sees ask manager', () => {
  it('flags a restock line for a manager and still confirms the sale', async () => {
    let body: unknown;
    const { calls } = renderAt('/voice', {
      messages,
      handlers: [
        shops('cashier'),
        ...voiceHandlers(draft('ready', [line('a'), line('b', { kind: 'restock', quantity: 10, disabled: 'ask_manager', heard: '10 milk came' })])),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${VOICE}/drafts/d1/confirm`) return undefined;
          body = c.body;
          return json(200, {
            draftId: 'd1',
            status: 'ready',
            lines: [
              { lineId: 'a', outcome: 'accepted' },
              { lineId: 'b', outcome: 'not_permitted' },
            ],
          });
        },
      ],
    });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    expect(within(list).getByText('Ask manager')).toBeInTheDocument();
    expect(within(list).getByText('Only a manager or owner can confirm this. It waits on this draft for them.')).toBeInTheDocument();
    expect(screen.getByText('1 line needs a manager. It stays on this draft until a manager confirms it.')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Confirm 1 change' }));
    // The server applied the sale and kept the draft open for the restock.
    expect(await screen.findByText('Ask a manager to confirm the rest')).toBeInTheDocument();
    expect(screen.getByText('1 change applied, 1 left for a manager.')).toBeInTheDocument();
    expect(screen.getByText('Needs a manager')).toBeInTheDocument();
    expect(body).toEqual({
      lines: [
        { lineId: 'a', outcome: 'accepted' },
        { lineId: 'b', outcome: 'accepted' },
      ],
    });
    const confirm = calls.find((c) => c.path.endsWith('/confirm'));
    expect(confirm?.headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('reopens a draft a cashier confirmed in part: done lines are shown, the cashier can only speak again', async () => {
    const { calls, router } = renderAt(`/voice?draft=${PARTIAL}`, { messages, handlers: [shops('cashier'), ...partialHandlers('cashier')] });
    const done = await screen.findByRole('region', { name: 'Already done' }, { timeout: 3000 });
    expect(within(done).getByText('Item a')).toBeInTheDocument();
    expect(within(done).getByText('Applied')).toBeInTheDocument();
    const list = screen.getByRole('list', { name: 'Draft lines' });
    expect(within(list).queryByText('Item a')).not.toBeInTheDocument();
    expect(within(list).getByText('Ask manager')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Discard' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Confirm/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Speak again' }));
    expect(await screen.findByRole('button', { name: 'Hold to talk' })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(calls.some((c) => c.path.endsWith('/confirm'))).toBe(false);
  });

  it('lets a manager finish it: only the open line is sent', async () => {
    let body: unknown;
    renderAt(`/voice?draft=${PARTIAL}`, {
      messages,
      handlers: [
        shops('manager'),
        ...partialHandlers('manager'),
        (c) => {
          if (c.method !== 'POST' || c.path !== `${VOICE}/drafts/${PARTIAL}/confirm`) return undefined;
          body = c.body;
          return json(200, {
            draftId: PARTIAL,
            status: 'confirmed',
            lines: [
              { lineId: 'a', outcome: 'accepted' },
              { lineId: 'b', outcome: 'accepted' },
            ],
          });
        },
      ],
    });
    await screen.findByRole('region', { name: 'Already done' }, { timeout: 3000 });
    const list = screen.getByRole('list', { name: 'Draft lines' });
    expect(within(list).queryByText('Ask manager')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm 1 change' }));
    expect(await screen.findByText('Draft confirmed')).toBeInTheDocument();
    expect(body).toEqual({ lines: [{ lineId: 'b', outcome: 'accepted' }] });
  });

  it('gives a manager every kind without the flag', async () => {
    renderAt('/voice', {
      messages,
      handlers: [shops('manager'), ...voiceHandlers(draft('ready', [line('a', { kind: 'restock', quantity: 10 }), line('b', { kind: 'set_price', quantity: null, pricePaise: 6000 })]))],
    });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    expect(within(list).queryByText('Ask manager')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm 2 changes' })).toBeEnabled();
  });
});

describe('SC-VO-05 an unmatched line needs an item', () => {
  it('highlights it, blocks Confirm, and Pick item searches the shop and fills the line', async () => {
    let body: unknown;
    renderAt('/voice', {
      messages,
      handlers: [
        shops('cashier'),
        ...voiceHandlers(draft('ready', [line('a', { storeItemId: null, itemName: 'maggi', matched: false, onHand: null, currentPricePaise: null, confidence: 0.4 })])),
        (c) =>
          c.method === 'GET' && c.path === `${BASE}/items`
            ? json(200, {
                items: [
                  {
                    id: 'i9',
                    storeId: 's1',
                    productId: 'p9',
                    name: 'Maggi noodles',
                    productName: 'Maggi noodles',
                    localName: null,
                    brand: null,
                    variant: null,
                    packSize: 70,
                    unit: 'g',
                    categoryId: 'c1',
                    barcodes: [],
                    pricePaise: 1400,
                    mrpPaise: null,
                    productMrpPaise: 1500,
                    onHand: 20,
                    reserved: 0,
                    available: 20,
                    lowStockAt: null,
                    lowStock: false,
                    isListed: true,
                    stockUpdatedAt: '2026-10-24T08:00:00.000Z',
                    stale: false,
                    createdAt: '2026-10-01T08:00:00.000Z',
                    updatedAt: '2026-10-24T08:00:00.000Z',
                  },
                ],
                nextCursor: null,
              })
            : undefined,
        (c) => {
          if (c.method !== 'POST' || !c.path.endsWith('/confirm')) return undefined;
          body = c.body;
          return json(200, { draftId: 'd1', status: 'confirmed', lines: [{ lineId: 'a', outcome: 'edited' }] });
        },
      ],
    });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    expect(within(list).getByText('Pick item', { selector: 'span' })).toBeInTheDocument();
    expect(screen.getByText('1 line needs an item or a number before you can confirm.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();

    fireEvent.click(within(list).getByRole('button', { name: 'Pick item' }));
    const sheet = await screen.findByRole('dialog', { name: 'Pick the item' });
    fireEvent.click(await within(sheet).findByRole('button', { name: 'Pick Maggi noodles · 70 g' }));
    fireEvent.click(within(sheet).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(within(list).getByText('Maggi noodles · 70 g')).toBeInTheDocument();
    expect(within(list).getByText('Edited')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm 1 change' }));
    expect(await screen.findByText('Draft confirmed')).toBeInTheDocument();
    expect(body).toEqual({ lines: [{ lineId: 'a', outcome: 'edited', value: { kind: 'sale', storeItemId: 'i9', quantity: 2 } }] });
  });
});

describe('SC-VO-06 editing a line', () => {
  it('checks the number before saving and turns a sale into a count of 0', async () => {
    let body: unknown;
    renderAt('/voice', {
      messages,
      handlers: [
        shops('cashier'),
        ...voiceHandlers(draft('ready', [line('a')])),
        (c) => {
          if (c.method !== 'POST' || !c.path.endsWith('/confirm')) return undefined;
          body = c.body;
          return json(200, { draftId: 'd1', status: 'confirmed', lines: [{ lineId: 'a', outcome: 'edited' }] });
        },
      ],
    });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    fireEvent.click(within(list).getByRole('button', { name: 'Edit' }));
    const sheet = await screen.findByRole('dialog', { name: 'Edit line' });
    fireEvent.change(within(sheet).getByLabelText('Quantity'), { target: { value: '0' } });
    fireEvent.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('Enter a whole number from 1 to 9999.')).toBeInTheDocument();

    fireEvent.click(within(sheet).getByRole('radio', { name: 'Count' }));
    expect(within(sheet).getByText('0 means out of stock.')).toBeInTheDocument();
    fireEvent.click(within(sheet).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(within(list).getByText('Out of stock', { exact: false })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Confirm 1 change' }));
    expect(await screen.findByText('Draft confirmed')).toBeInTheDocument();
    expect(body).toEqual({ lines: [{ lineId: 'a', outcome: 'edited', value: { kind: 'count', storeItemId: 'i-a', quantity: 0 } }] });
  });

  it("warns a cashier who turns a line into a restock that it won't be applied", async () => {
    renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('ready', [line('a')]))] });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    fireEvent.click(within(list).getByRole('button', { name: 'Edit' }));
    const sheet = await screen.findByRole('dialog', { name: 'Edit line' });
    fireEvent.click(within(sheet).getByRole('radio', { name: 'Restock' }));
    expect(within(sheet).getByText('Only a manager or owner can confirm this. It waits on this draft for them.')).toBeInTheDocument();
  });
});

describe('SC-VO-11 discard', () => {
  it('Discard sends every line discarded and nothing changes', async () => {
    let body: unknown;
    renderAt('/voice', {
      messages,
      handlers: [
        shops('owner'),
        ...voiceHandlers(draft('ready', [line('a'), line('b')])),
        (c) => {
          if (c.method !== 'POST' || !c.path.endsWith('/confirm')) return undefined;
          body = c.body;
          return json(200, {
            draftId: 'd1',
            status: 'discarded',
            lines: [
              { lineId: 'a', outcome: 'discarded' },
              { lineId: 'b', outcome: 'discarded' },
            ],
          });
        },
      ],
    });
    await speak();
    await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(await screen.findByText('Draft discarded. Nothing changed.')).toBeInTheDocument();
    expect(body).toEqual({
      lines: [
        { lineId: 'a', outcome: 'discarded' },
        { lineId: 'b', outcome: 'discarded' },
      ],
    });
  });

  it('Remove drops one line and Undo brings it back', async () => {
    renderAt('/voice', { messages, handlers: [shops('owner'), ...voiceHandlers(draft('ready', [line('a'), line('b')]))] });
    await speak();
    const list = await screen.findByRole('list', { name: 'Draft lines' }, { timeout: 3000 });
    fireEvent.click(within(list).getAllByRole('button', { name: 'Remove' })[0] as HTMLElement);
    expect(screen.getByRole('button', { name: 'Confirm 1 change' })).toBeEnabled();
    fireEvent.click(within(list).getByRole('button', { name: 'Undo' }));
    expect(screen.getByRole('button', { name: 'Confirm 2 changes' })).toBeEnabled();
  });
});

describe('SC-VO-08/09/10 refusals and failures', () => {
  it('says so when the daily AI cap is reached', async () => {
    renderAt('/voice', {
      messages,
      handlers: [
        shops('cashier'),
        (c) => (c.method === 'GET' && c.path === VOICE ? json(200, AVAILABLE) : undefined),
        (c) => (c.method === 'POST' && c.path === `${VOICE}/drafts` ? problem(429, 'ai_daily_cap_reached') : undefined),
      ],
    });
    await speak();
    expect(await screen.findByText("Voice update has reached today's limit for this shop. You can still update stock on the “Stock” page.")).toBeInTheDocument();
  });

  it('refuses a recording above the size limit without sending it', async () => {
    FakeRecorder.size = AVAILABLE.maxBytes + 1;
    const { calls } = renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('ready', [line('a')]))] });
    await speak();
    expect(await screen.findByText('That recording is too long. Try a shorter one.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('shows a failed draft with no words heard and lets the person speak again', async () => {
    renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('failed', [], { failure: 'empty' }))] });
    await speak();
    expect(await screen.findByText("We didn't catch any words. Hold the button and speak again.", {}, { timeout: 3000 })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Speak again' }));
    expect(await screen.findByRole('button', { name: 'Hold to talk' })).toBeInTheDocument();
  });

  it('reads a provider timeout as busy', async () => {
    renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('failed', [], { failure: 'timeout' }))] });
    await speak();
    expect(await screen.findByText('Voice update is busy right now. Try again in a minute.', {}, { timeout: 3000 })).toBeInTheDocument();
  });

  it('asks for the microphone when it is refused', async () => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: () => Promise.reject(new Error('NotAllowedError')) },
    });
    renderAt('/voice', { messages, handlers: [shops('cashier'), ...voiceHandlers(draft('ready'))] });
    const btn = await screen.findByRole('button', { name: 'Hold to talk' });
    fireEvent.keyDown(btn, { key: ' ' });
    expect(await screen.findByText('Allow the microphone for this site, then hold the button again.')).toBeInTheDocument();
  });

  it('shows the draft as confirmed when it is opened again after Confirm', async () => {
    renderAt('/voice', {
      messages,
      handlers: [shops('cashier'), ...voiceHandlers(draft('confirmed', [line('a')], { outcomes: [{ lineId: 'a', outcome: 'accepted' }] }))],
    });
    await speak();
    expect(await screen.findByText('Applied', {}, { timeout: 3000 })).toBeInTheDocument();
  });
});
