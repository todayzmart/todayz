import { ApiError } from '@todayz/api-client';
import { describe, expect, it } from 'vitest';
import type { PosApi, SaleAck, SyncRequest, SyncResponse } from '../pos/api';
import type { CatalogueSnapshot, Sale } from '../pos/types';
import { backoffMs, createSyncEngine, KEEP_SYNCED } from './engine';
import { memoryStorage } from './storage';

const SETTINGS = { creditMinOrderValuePaise: 10000, reversalWindowHours: 24, offlineLinkMaxHours: 12 };

function problem(status: number, code: string, headers: Record<string, string> = {}) {
  return new ApiError({ type: 'about:blank', title: code, status, code }, new Headers(headers));
}

function sale(id: string, over: Partial<Sale> = {}): Omit<Sale, 'deviceSeq' | 'deviceId'> {
  return {
    clientId: id,
    soldAt: '2026-10-24T10:00:00.000Z',
    lines: [{ clientId: `${id}-l1`, storeItemId: 'item-1', name: 'Rice 1 kg', unitPricePaise: 5000, quantity: 2, discountPaise: 0, lineTotalPaise: 10000 }],
    subtotalPaise: 10000,
    billDiscountPaise: 0,
    roundOffPaise: 0,
    creditsUsed: 0,
    totalPaise: 10000,
    paymentMethod: 'cash',
    link: null,
    ...over,
  };
}

interface Server {
  api: PosApi;
  syncCalls: { body: SyncRequest; key: string }[];
  applied: Map<string, SaleAck>;
  /** Next sync calls throw these, in order, before answering normally. */
  failWith: unknown[];
  /** Answer, then throw (the response is lost on the way back). */
  loseResponses: number;
  respond: (clientId: string) => Partial<SaleAck> & { status?: 'rejected'; code?: string };
  snapshot: CatalogueSnapshot;
  changesCalls: string[];
  changesError: unknown;
}

function fakeServer(): Server {
  const s: Server = {
    syncCalls: [],
    applied: new Map(),
    failWith: [],
    loseResponses: 0,
    respond: () => ({}),
    changesCalls: [],
    changesError: null,
    snapshot: { items: [], cursor: 'c-snap', settings: SETTINGS, serverNow: '2026-10-24T10:00:00.000Z' },
    api: undefined as unknown as PosApi,
  };
  const ack = (clientId: string): SaleAck => ({
    saleId: `sale-${clientId}`,
    receiptToken: `tok-${clientId}`,
    roundOffPaise: 0,
    totalPaise: 10000,
    serverNow: '2026-10-24T10:00:00.000Z',
    ...s.respond(clientId),
  });
  s.api = {
    getSnapshot: () => Promise.resolve(s.snapshot),
    getChanges: (cursor) => {
      s.changesCalls.push(cursor);
      if (s.changesError) return Promise.reject(s.changesError);
      return Promise.resolve({ changes: [], cursor: 'c-next', settings: SETTINGS, serverNow: '2026-10-24T10:00:00.000Z' });
    },
    sync: (body, key) => {
      s.syncCalls.push({ body: structuredClone(body), key });
      const f = s.failWith.shift();
      if (f) return Promise.reject(f);
      const results: SyncResponse['results'] = body.entries.map((e) => {
        const r = s.respond(e.clientId);
        if (r.status === 'rejected') return { clientId: e.clientId, status: 'rejected', code: r.code ?? 'rejected' };
        const dup = s.applied.has(e.clientId);
        const a = dup ? s.applied.get(e.clientId) as SaleAck : ack(e.clientId);
        s.applied.set(e.clientId, a);
        return { clientId: e.clientId, status: dup ? 'duplicate' : 'applied', ...a };
      });
      const res: SyncResponse = { results, changes: [], cursor: 'c-sync', serverNow: '2026-10-24T10:00:00.000Z' };
      if (s.loseResponses > 0) {
        s.loseResponses -= 1;
        return Promise.reject(new TypeError('network'));
      }
      return Promise.resolve(res);
    },
    createSale: (wire) => {
      const f = s.failWith.shift();
      if (f) return Promise.reject(f);
      const a = ack(wire.clientId);
      s.applied.set(wire.clientId, a);
      return Promise.resolve(a);
    },
    reverseSale: () => Promise.reject(new Error('unused')),
    createBillClaim: () => Promise.reject(new Error('unused')),
    getBillClaim: () => Promise.reject(new Error('unused')),
    verifyCounterCode: () => Promise.reject(new Error('unused')),
  };
  return s;
}

function setup(over: { online?: boolean } = {}) {
  const storage = memoryStorage();
  const server = fakeServer();
  let t = Date.parse('2026-10-24T10:00:00.000Z');
  let n = 0;
  let isOnline = over.online ?? true;
  const make = () =>
    createSyncEngine({
      storage,
      api: server.api,
      deviceId: 'dev-1',
      newId: () => `id-${++n}`,
      now: () => t,
      isOnline: () => isOnline,
      random: () => 0.5,
    });
  return {
    storage,
    server,
    make,
    engine: make(),
    advance: (ms: number) => void (t += ms),
    setOnline: (v: boolean) => void (isOnline = v),
  };
}

describe('outbox and replay', () => {
  it('assigns increasing device sequence numbers and sends in order', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    await engine.recordSale(sale('b'));
    await engine.recordSale(sale('c'));
    await engine.flush();
    expect(server.syncCalls).toHaveLength(1);
    expect(server.syncCalls[0]?.body.entries.map((e) => [e.clientId, e.seq])).toEqual([['a', 1], ['b', 2], ['c', 3]]);
    const entries = await engine.entries();
    expect(entries.every((e) => e.state === 'synced')).toBe(true);
    expect(entries[0]?.result?.receiptToken).toBe('tok-a');
    expect(engine.state().pending).toBe(0);
  });

  it('keeps sales while offline and sends them when the connection returns', async () => {
    const { engine, server, setOnline } = setup({ online: false });
    await engine.recordSale(sale('a'));
    await engine.recordSale(sale('b'));
    await engine.flush();
    expect(server.syncCalls).toHaveLength(0);
    expect(engine.state()).toMatchObject({ status: 'offline', pending: 2 });
    setOnline(true);
    await engine.flush();
    expect(server.syncCalls).toHaveLength(1);
    expect(engine.state().pending).toBe(0);
  });

  it('survives a reload: a new engine on the same storage sends what is waiting', async () => {
    const { engine, server, storage } = setup({ online: false });
    await engine.recordSale(sale('a'));
    // A new engine on the same storage, as after a reload (the storage is IndexedDB in the browser).
    const online = createSyncEngine({ storage, api: server.api, deviceId: 'dev-1', newId: () => 'k1' });
    await online.flush();
    expect(server.syncCalls[0]?.body.entries.map((e) => e.clientId)).toEqual(['a']);
  });

  it('batches by 50 and keeps sequence order across batches', async () => {
    const { engine, server } = setup();
    for (let i = 0; i < 120; i += 1) await engine.recordSale(sale(`s${String(i).padStart(3, '0')}`));
    await engine.flush();
    expect(server.syncCalls.map((c) => c.body.entries.length)).toEqual([50, 50, 20]);
    const seqs = server.syncCalls.flatMap((c) => c.body.entries.map((e) => e.seq));
    expect(seqs).toEqual([...seqs].sort((a, b) => a - b));
    expect(new Set(server.syncCalls.map((c) => c.key)).size).toBe(3);
  });
});

describe('duplicates and lost answers', () => {
  it('retries a lost answer with the same key and the same body, and counts a duplicate as done', async () => {
    const { engine, server, advance } = setup();
    await engine.recordSale(sale('a'));
    server.loseResponses = 1; // the server applied it, the answer never arrived
    await engine.flush();
    expect(engine.state().status).toBe('backoff');
    expect((await engine.entries())[0]?.state).toBe('sending');

    advance(backoffMs(1, 0.5) + 1);
    await engine.flush();
    expect(server.syncCalls).toHaveLength(2);
    expect(server.syncCalls[1]?.key).toBe(server.syncCalls[0]?.key);
    expect(server.syncCalls[1]?.body).toEqual(server.syncCalls[0]?.body);
    const [e] = await engine.entries();
    expect(e?.state).toBe('synced');
    expect(e?.result?.saleId).toBe('sale-a');
    expect(server.applied.size).toBe(1);
  });

  it('resends the same in-flight batch after a reload, even with new sales waiting', async () => {
    const { engine, server, make, advance } = setup();
    await engine.recordSale(sale('a'));
    server.loseResponses = 1;
    await engine.flush();
    const firstKey = server.syncCalls[0]?.key;

    const reloaded = make();
    await reloaded.recordSale(sale('b'));
    advance(10 * 60 * 1000);
    await reloaded.flush();
    expect(server.syncCalls[1]?.key).toBe(firstKey);
    expect(server.syncCalls[1]?.body.entries.map((e) => e.clientId)).toEqual(['a']);
    expect(server.syncCalls[2]?.body.entries.map((e) => e.clientId)).toEqual(['b']);
    expect((await reloaded.entries()).map((e) => e.state)).toEqual(['synced', 'synced']);
  });

  it('does not send the same sale twice when two flushes overlap', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    await Promise.all([engine.flush(), engine.flush()]);
    expect(server.syncCalls).toHaveLength(1);
  });
});

describe('failure and backoff', () => {
  it('backs off after a server error, honours the wait, and recovers', async () => {
    const { engine, server, advance } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [problem(503, 'unavailable')];
    await engine.flush();
    const wait = backoffMs(1, 0.5);
    expect(engine.state()).toMatchObject({ status: 'backoff', attempts: 1, lastError: 'unavailable' });
    expect(engine.nextAttemptAt()).toBe(Date.parse('2026-10-24T10:00:00.000Z') + wait);

    await engine.flush(); // too early: quiet
    expect(server.syncCalls).toHaveLength(1);

    advance(wait);
    await engine.flush();
    expect(server.syncCalls).toHaveLength(2);
    expect(engine.state()).toMatchObject({ status: 'idle', pending: 0, attempts: 0, nextAttemptAt: null });
  });

  it('grows the wait with each failure up to five minutes', () => {
    expect(backoffMs(1, 0.5)).toBe(1000);
    expect(backoffMs(2, 0.5)).toBe(2000);
    expect(backoffMs(4, 0.5)).toBe(8000);
    expect(backoffMs(30, 0.5)).toBe(5 * 60 * 1000);
    expect(backoffMs(30, 1)).toBeLessThanOrEqual(Math.round(5 * 60 * 1000 * 1.25));
  });

  it('uses Retry-After when the server sends it', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [problem(429, 'rate_limited', { 'retry-after': '30' })];
    await engine.flush();
    expect(engine.nextAttemptAt()).toBe(Date.parse('2026-10-24T10:00:00.000Z') + 30_000);
  });

  it('force-sync ignores the wait', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [new TypeError('network')];
    await engine.flush();
    expect(engine.state().lastError).toBe('network');
    await engine.flush({ force: true });
    expect(server.syncCalls).toHaveLength(2);
    expect(engine.state().pending).toBe(0);
  });

  it('finds the refused sale in a batch, rejects only that one and sends the rest', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    await engine.recordSale(sale('b'));
    await engine.recordSale(sale('c'));
    // The whole batch is refused (validation) and then entries go one at a time; only b is bad.
    const real = server.api.sync;
    server.api.sync = (body, key) => {
      if (body.entries.some((e) => e.clientId === 'b')) {
        server.syncCalls.push({ body: structuredClone(body), key });
        return Promise.reject(problem(422, 'validation_failed'));
      }
      return real(body, key);
    };
    await engine.flush();
    const states = Object.fromEntries((await engine.entries()).map((e) => [e.clientId, e.state]));
    expect(states).toEqual({ a: 'synced', b: 'rejected', c: 'synced' });
    expect(engine.state()).toMatchObject({ pending: 0, rejected: 1 });
    const b = (await engine.entries()).find((e) => e.clientId === 'b');
    expect(b?.lastError).toBe('validation_failed');
  });

  it('a rejected sale reported in the results is kept for a person, and can be retried or dismissed', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.respond = () => ({ status: 'rejected', code: 'forbidden' });
    await engine.flush();
    expect((await engine.entries())[0]).toMatchObject({ state: 'rejected', lastError: 'forbidden' });
    server.respond = () => ({});
    await engine.retry('a');
    await engine.flush();
    expect((await engine.entries())[0]?.state).toBe('synced');
    await engine.dismiss('a');
    expect(await engine.entries()).toHaveLength(0);
  });

  it('starts a fresh key when the server says the key was used for a different body', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [problem(422, 'idempotency_mismatch')];
    await engine.flush();
    expect(server.syncCalls).toHaveLength(2);
    expect(server.syncCalls[1]?.key).not.toBe(server.syncCalls[0]?.key);
    expect(engine.state().pending).toBe(0);
  });
});

describe('conflicts', () => {
  it('shows the server price when it differs and keeps the sold lines as returned', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.respond = () => ({
      totalPaise: 9000,
      roundOffPaise: 0,
      lines: [{ clientId: 'a-l1', unitPricePaise: 4500, discountPaise: 0, lineTotalPaise: 9000 }],
    });
    await engine.flush();
    const [e] = await engine.entries();
    const s = e?.payload as Sale;
    expect(e?.result?.serverTotalPaise).toBe(9000);
    expect(s.totalPaise).toBe(9000);
    expect(s.lines[0]?.unitPricePaise).toBe(4500);
  });

  it('keeps the device total when the server agrees', async () => {
    const { engine } = setup();
    await engine.recordSale(sale('a'));
    await engine.flush();
    expect((await engine.entries())[0]?.result?.serverTotalPaise).toBeNull();
  });

  it('flags stock that went below zero ("count needed")', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.respond = () => ({ stockFlags: ['item-1'] });
    await engine.flush();
    expect((await engine.entries())[0]?.result?.stockFlags).toEqual(['item-1']);
  });

  it('records a failed customer link and drops the typed counter code from the device', async () => {
    const { engine, server } = setup();
    await engine.recordSale(
      sale('a', { link: { method: 'counter_code', phone: '+919000000000', code: '4821', codeAt: '2026-10-24T09:59:00.000Z', clockOffsetMs: 1200 } }),
    );
    server.respond = () => ({ linkStatus: 'failed' });
    await engine.flush();
    const [e] = await engine.entries();
    expect(e?.result?.linkStatus).toBe('failed');
    expect((e?.payload as Sale).link).toMatchObject({ method: 'counter_code', code: '' });
    // The device never sends the code on the wire after it was spent.
    expect(JSON.stringify(server.syncCalls[0]?.body)).toContain('"code":"4821"');
    expect(JSON.stringify(e)).not.toContain('4821');
  });

  it('sends the device clock offset with the sync so the server can check the code time', async () => {
    const { engine, server, storage } = setup();
    await storage.setClockCheck({ offsetMs: -2500, at: '2026-10-24T09:00:00.000Z' });
    await engine.recordSale(sale('a'));
    await engine.flush();
    expect(server.syncCalls[0]?.body.clockOffsetMs).toBe(-2500);
  });
});

describe('catalogue', () => {
  it('downloads a snapshot first, then deltas', async () => {
    const { engine, server, storage } = setup();
    server.snapshot.items = [{ id: 'i1', name: 'Rice', variant: null, packSize: '1', unit: 'kg', barcodes: [], pricePaise: 5000, available: true, isListed: true }] as never;
    await engine.refreshCatalogue();
    expect((await storage.loadCatalogue()).meta?.cursor).toBe('c-snap');
    await engine.refreshCatalogue();
    expect(server.changesCalls).toEqual(['c-snap']);
    expect((await storage.loadCatalogue()).meta?.cursor).toBe('c-next');
  });

  it('falls back to a full snapshot when the cursor is too old (410, D-36)', async () => {
    const { engine, server, storage } = setup();
    await engine.refreshCatalogue();
    server.changesError = problem(410, 'resync_required');
    server.snapshot.cursor = 'c-fresh';
    await engine.refreshCatalogue();
    expect((await storage.loadCatalogue()).meta?.cursor).toBe('c-fresh');
  });

  it('takes a snapshot and retries when sync answers 410, with entries untouched', async () => {
    const { engine, server, storage } = setup();
    await engine.refreshCatalogue();
    await engine.recordSale(sale('a'));
    server.failWith = [problem(410, 'resync_required')];
    server.snapshot.cursor = 'c-fresh';
    await engine.flush();
    expect(server.syncCalls).toHaveLength(2);
    expect((await engine.entries())[0]?.state).toBe('synced');
    // The retry's answer moved the cursor to c-sync, then the idle refresh pulled the next delta.
    expect((await storage.loadCatalogue()).meta?.cursor).toBe('c-next');
  });
});

describe('online sale path', () => {
  it('records the server answer at once', async () => {
    const { engine } = setup();
    await engine.recordSale(sale('a'));
    const e = await engine.sendSaleNow('a');
    expect(e).toMatchObject({ state: 'synced', result: { receiptToken: 'tok-a' } });
  });

  it('leaves the sale in the outbox on a network failure, and the flush then sends it once', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [new TypeError('network')];
    const e = await engine.sendSaleNow('a');
    expect(e?.state).toBe('pending');
    await engine.flush({ force: true });
    expect((await engine.entries())[0]?.state).toBe('synced');
  });

  it('marks the sale refused when the server says no', async () => {
    const { engine, server } = setup();
    await engine.recordSale(sale('a'));
    server.failWith = [problem(403, 'forbidden')];
    const e = await engine.sendSaleNow('a');
    expect(e).toMatchObject({ state: 'rejected', lastError: 'forbidden' });
  });
});

describe('reversal', () => {
  it('reverses a synced sale once, under a key fixed by the sale, and marks it', async () => {
    const { engine, server } = setup();
    const keys: string[] = [];
    server.api.reverseSale = (saleId, key) => {
      keys.push(`${saleId}:${key}`);
      return Promise.resolve({ stockReturned: [{ storeItemId: 'item-1', quantity: 2 }], creditsReturned: 0, cashbackClawedBack: 0 });
    };
    await engine.recordSale(sale('a'));
    await engine.flush();
    const ack = await engine.reverse('a');
    expect(ack.stockReturned).toHaveLength(1);
    expect((await engine.entries())[0]?.result?.reversed).toBe(true);
    await engine.reverse('a');
    expect(new Set(keys).size).toBe(1);
  });

  it('refuses a sale that has not synced', async () => {
    const { engine } = setup({ online: false });
    await engine.recordSale(sale('a'));
    await expect(engine.reverse('a')).rejects.toThrow();
  });
});

describe('housekeeping', () => {
  it('keeps the newest synced sales for the history and drops older ones', async () => {
    const { engine } = setup();
    for (let i = 0; i < KEEP_SYNCED + 30; i += 1) await engine.recordSale(sale(`s${String(i).padStart(4, '0')}`));
    await engine.flush();
    const entries = await engine.entries();
    expect(entries).toHaveLength(KEEP_SYNCED);
    expect(entries[0]?.clientId).toBe('s0030');
  });

  it('never prunes a sale that has not synced', async () => {
    const { engine, server, setOnline } = setup();
    await engine.recordSale(sale('a'));
    setOnline(false);
    await engine.flush();
    expect(server.syncCalls).toHaveLength(0);
    expect((await engine.entries())[0]?.state).toBe('pending');
  });
});
