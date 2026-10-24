// The sync engine (W2-11; 02 §4.3, 04 "POS and sync").
//
// Writes go to the outbox first (a client ULID, the next device sequence number), so nothing is lost
// to a reload or a dropped connection. The engine then sends entries to POST {S}/sync in `seq` order:
//
//  - A batch is sent under one Idempotency-Key and is remembered (`inflight`). If the answer is lost,
//    the retry sends exactly the same entries under the same key, so a replay can never double a sale.
//    The server also dedupes by `(store_id, client_id)`, so a sale already applied comes back as
//    `duplicate`, which counts as done.
//  - Failures back off: 1 s doubling to 5 min, with jitter, and `Retry-After` is honoured. The engine
//    has no timers; the caller asks `nextAttemptAt()` and calls `flush()`.
//  - A request the server refuses outright (4xx other than the retryable ones) is split: one entry per
//    request until the bad one is found; that entry is `rejected` and later entries still go.
//  - A sale never conflicts away: stock may go negative (flagged "count needed"), a counter-code link
//    may fail at sync ("Customer link failed"), and the server's price is shown as returned.
//  - `410 resync_required` (cursor older than 30 days, D-36) downloads a full catalogue snapshot first.
import { ApiError } from '@todayz/api-client';
import { entryToWire, saleToWire, type PosApi, type SaleAck, type SyncResult } from '../pos/api';
import type { CatalogueChange, CatalogueSnapshot, Sale } from '../pos/types';
import type { PosStorage } from './storage';
import { SYNC_BATCH_MAX, type EntryResult, type OutboxEntry, type StockMovement } from './types';

export type SyncStatus = 'idle' | 'syncing' | 'offline' | 'backoff' | 'resync';

export interface SyncState {
  status: SyncStatus;
  /** Entries that still have to reach the server (pending or sending). */
  pending: number;
  /** Entries the server refused; a person looks at them. */
  rejected: number;
  lastSyncedAt: string | null;
  lastError: string | null;
  /** Epoch ms of the next automatic attempt while backing off. */
  nextAttemptAt: number | null;
  attempts: number;
  /** Goes up whenever the stored catalogue changes, so the screen reloads it. */
  catalogueVersion: number;
}

export interface EngineDeps {
  storage: PosStorage;
  api: PosApi;
  deviceId: string;
  newId: () => string;
  now?: () => number;
  isOnline?: () => boolean;
  random?: () => number;
}

/** Synced entries kept on the device for the sales history; older ones are dropped. */
export const KEEP_SYNCED = 200;
const BACKOFF_BASE_MS = 1000;
const BACKOFF_MAX_MS = 5 * 60 * 1000;

export function backoffMs(attempt: number, random = 0.5): number {
  const base = Math.min(BACKOFF_MAX_MS, BACKOFF_BASE_MS * 2 ** Math.max(0, attempt - 1));
  return Math.round(base * (0.75 + random * 0.5));
}

/** No answer from the server at all (offline, DNS, reset), as opposed to an ApiError. */
export function isNetworkError(e: unknown): boolean {
  return !(e instanceof ApiError);
}

/** A failure that may succeed later with the same request. */
export function isRetryable(e: unknown): boolean {
  if (!(e instanceof ApiError)) return true;
  if (e.status >= 500 || e.status === 408 || e.status === 429) return true;
  // 409 conflict = another request holds the idempotency lease; 401 = the session is refreshing.
  return e.status === 409 && e.code === 'conflict' ? true : e.status === 401;
}

export function createSyncEngine(deps: EngineDeps) {
  const { storage, api, deviceId, newId } = deps;
  const now = deps.now ?? Date.now;
  const online = deps.isOnline ?? (() => true);
  const random = deps.random ?? Math.random;

  let state: SyncState = {
    status: 'idle',
    pending: 0,
    rejected: 0,
    lastSyncedAt: null,
    lastError: null,
    nextAttemptAt: null,
    attempts: 0,
    catalogueVersion: 0,
  };
  let singleMode = false;
  let chain: Promise<unknown> = Promise.resolve();
  const listeners = new Set<(s: SyncState) => void>();

  /** Runs operations one at a time, so the direct sale path and a flush never touch an entry together. */
  function exclusive<T>(fn: () => Promise<T>): Promise<T> {
    const run = chain.then(fn, fn);
    chain = run.catch(() => undefined);
    return run;
  }

  function set(patch: Partial<SyncState>) {
    state = { ...state, ...patch };
    for (const l of listeners) l(state);
  }

  async function recount() {
    const entries = await storage.listEntries();
    set({
      pending: entries.filter((e) => e.state === 'pending' || e.state === 'sending').length,
      rejected: entries.filter((e) => e.state === 'rejected').length,
    });
  }

  async function clockOffsetMs(): Promise<number | null> {
    return (await storage.getClockCheck())?.offsetMs ?? null;
  }

  async function noteServerTime(serverNow: string) {
    const t = Date.parse(serverNow);
    if (Number.isFinite(t)) await storage.setClockCheck({ offsetMs: now() - t, at: new Date(now()).toISOString() });
  }

  async function newEntry(kind: 'sale' | 'movement', clientId: string, payload: Sale | StockMovement, seq: number): Promise<OutboxEntry> {
    const entry: OutboxEntry = {
      clientId,
      seq,
      kind,
      createdAt: new Date(now()).toISOString(),
      state: 'pending',
      attempts: 0,
      lastError: null,
      payload,
    };
    await storage.putEntry(entry);
    return entry;
  }

  /** Records a sale in the outbox. The caller gives everything except the device sequence number. */
  async function recordSale(sale: Omit<Sale, 'deviceSeq' | 'deviceId'>): Promise<OutboxEntry> {
    const seq = await storage.nextSeq();
    const entry = await newEntry('sale', sale.clientId, { ...sale, deviceId, deviceSeq: seq }, seq);
    await recount();
    return entry;
  }

  async function recordMovement(m: StockMovement): Promise<OutboxEntry> {
    const seq = await storage.nextSeq();
    const entry = await newEntry('movement', newId(), m, seq);
    await recount();
    return entry;
  }

  function stripCode(sale: Sale): Sale {
    if (sale.link?.method !== 'counter_code') return sale;
    // The typed code is only kept until the server has verified it (D-12); it is never shown or logged.
    return { ...sale, link: { ...sale.link, code: '' } };
  }

  function applyAckToSale(sale: Sale, ack: Partial<SaleAck>): { sale: Sale; serverTotalPaise: number | null } {
    let next = stripCode(sale);
    let serverTotalPaise: number | null = null;
    if (ack.lines?.length) {
      const byId = new Map(ack.lines.map((l) => [l.clientId, l]));
      next = {
        ...next,
        lines: next.lines.map((l) => {
          const s = byId.get(l.clientId);
          return s ? { ...l, unitPricePaise: s.unitPricePaise, discountPaise: s.discountPaise, lineTotalPaise: s.lineTotalPaise } : l;
        }),
      };
    }
    if (typeof ack.totalPaise === 'number') {
      if (ack.totalPaise !== sale.totalPaise) serverTotalPaise = ack.totalPaise;
      next = {
        ...next,
        totalPaise: ack.totalPaise,
        roundOffPaise: ack.roundOffPaise ?? next.roundOffPaise,
        billDiscountPaise: ack.discountPaise ?? next.billDiscountPaise,
        creditsUsed: ack.creditsUsed ?? next.creditsUsed,
      };
    }
    return { sale: next, serverTotalPaise };
  }

  async function markApplied(entry: OutboxEntry, ack: Partial<SaleAck>) {
    const result: EntryResult = {
      saleId: ack.saleId ?? null,
      receiptToken: ack.receiptToken ?? null,
      linkStatus: ack.linkStatus ?? null,
      stockFlags: ack.stockFlags ?? [],
      cashbackCredits: ack.cashbackCredits ?? null,
    };
    let payload = entry.payload;
    if (entry.kind === 'sale') {
      const applied = applyAckToSale(entry.payload as Sale, ack);
      payload = applied.sale;
      result.serverTotalPaise = applied.serverTotalPaise;
    }
    await storage.putEntry({ ...entry, state: 'synced', lastError: null, payload, result });
  }

  async function markRejected(entry: OutboxEntry, code: string) {
    const payload = entry.kind === 'sale' ? stripCode(entry.payload as Sale) : entry.payload;
    await storage.putEntry({ ...entry, state: 'rejected', lastError: code, payload, result: { errorCode: code } });
  }

  async function prune() {
    const synced = (await storage.listEntries()).filter((e) => e.state === 'synced');
    if (synced.length > KEEP_SYNCED) await storage.removeEntries(synced.slice(0, synced.length - KEEP_SYNCED).map((e) => e.clientId));
  }

  async function applyChanges(changes: CatalogueChange[], cursor: string, settings?: CatalogueSnapshot['settings']) {
    const { meta } = await storage.loadCatalogue();
    const keep = settings ?? meta?.settings;
    if (!keep) return; // no snapshot yet; the next refresh downloads one
    const upserts = changes.flatMap((c) => (c.op === 'upsert' && c.item ? [c.item] : []));
    const deletes = changes.filter((c) => c.op === 'delete').map((c) => c.entityId);
    await storage.applyChanges(upserts, deletes, {
      cursor,
      settings: keep,
      syncedAt: new Date(now()).toISOString(),
    });
    set({ catalogueVersion: state.catalogueVersion + 1 });
  }

  async function downloadSnapshot() {
    set({ status: 'resync' });
    const snap = await api.getSnapshot();
    await storage.saveCatalogue(snap.items, { cursor: snap.cursor, settings: snap.settings, syncedAt: new Date(now()).toISOString() });
    set({ catalogueVersion: state.catalogueVersion + 1 });
    await noteServerTime(snap.serverNow);
  }

  /** Download the catalogue: a snapshot the first time, then deltas; a stale cursor falls back to a snapshot (D-36). */
  async function refreshCatalogue(): Promise<void> {
    if (!online()) return;
    const { meta } = await storage.loadCatalogue();
    if (!meta) return downloadSnapshot();
    try {
      const d = await api.getChanges(meta.cursor);
      await applyChanges(d.changes, d.cursor, d.settings);
      await noteServerTime(d.serverNow);
    } catch (e) {
      if (e instanceof ApiError && e.code === 'resync_required') return downloadSnapshot();
      throw e;
    }
  }

  function failure(e: unknown, attempts: number) {
    const retryAfter = e instanceof ApiError ? e.retryAfterSeconds : undefined;
    const wait = retryAfter !== undefined ? retryAfter * 1000 : backoffMs(attempts, random());
    set({
      status: 'backoff',
      attempts,
      lastError: e instanceof ApiError ? e.code : 'network',
      nextAttemptAt: now() + wait,
    });
  }

  async function sendBatch(): Promise<'more' | 'done'> {
    const entries = await storage.listEntries();
    const inflight = await storage.getInflight();
    let batch: OutboxEntry[];
    let key: string;
    if (inflight) {
      const byId = new Map(entries.map((e) => [e.clientId, e]));
      batch = inflight.clientIds.map((id) => byId.get(id)).filter((e): e is OutboxEntry => !!e && e.state !== 'synced' && e.state !== 'rejected');
      key = inflight.key;
    } else {
      batch = entries.filter((e) => e.state === 'pending').slice(0, singleMode ? 1 : SYNC_BATCH_MAX);
      key = newId();
    }
    if (batch.length === 0) {
      await storage.setInflight(null);
      return 'done';
    }
    if (!inflight) {
      await storage.setInflight({ key, clientIds: batch.map((e) => e.clientId) });
    }
    for (const e of batch) await storage.putEntry({ ...e, state: 'sending', attempts: e.attempts + 1 });

    const { meta } = await storage.loadCatalogue();
    try {
      const res = await api.sync(
        { deviceId, cursor: meta?.cursor ?? null, clockOffsetMs: await clockOffsetMs(), entries: batch.map(entryToWire) },
        key,
      );
      const results = new Map<string, SyncResult>(res.results.map((r) => [r.clientId, r]));
      for (const e of batch) {
        const r = results.get(e.clientId);
        const fresh = { ...e, state: 'sending' as const, attempts: e.attempts + 1 };
        if (!r) await storage.putEntry({ ...fresh, state: 'pending' });
        else if (r.status === 'rejected') await markRejected(fresh, r.code);
        else await markApplied(fresh, r);
      }
      await storage.setInflight(null);
      await applyChanges(res.changes, res.cursor, res.settings);
      await noteServerTime(res.serverNow);
      await prune();
      singleMode = false;
      set({ attempts: 0, lastError: null, nextAttemptAt: null, lastSyncedAt: new Date(now()).toISOString() });
      return 'more';
    } catch (e) {
      if (e instanceof ApiError && e.code === 'resync_required') {
        // Nothing was applied; a new key is fine.
        await storage.setInflight(null);
        for (const b of batch) await storage.putEntry({ ...b, state: 'pending' });
        await downloadSnapshot();
        return 'more';
      }
      if (e instanceof ApiError && e.code === 'idempotency_mismatch') {
        await storage.setInflight(null);
        for (const b of batch) await storage.putEntry({ ...b, state: 'pending' });
        return 'more';
      }
      if (isRetryable(e)) {
        for (const b of batch) await storage.putEntry({ ...b, state: 'sending', attempts: b.attempts + 1, lastError: e instanceof ApiError ? e.code : 'network' });
        failure(e, state.attempts + 1);
        return 'done';
      }
      // The server refused the request. Find the bad entry by sending one at a time.
      await storage.setInflight(null);
      const code = e instanceof ApiError ? e.code : 'rejected';
      const [only] = batch;
      if (only && batch.length === 1) {
        await markRejected({ ...only, attempts: only.attempts + 1 }, code);
        singleMode = false;
      } else {
        for (const b of batch) await storage.putEntry({ ...b, state: 'pending' });
        singleMode = true;
      }
      return 'more';
    }
  }

  /**
   * Sends what is waiting and pulls catalogue changes. Quiet when offline or while backing off (unless
   * `force`, which the "Sync now" button uses).
   */
  function flush(opts: { force?: boolean } = {}): Promise<void> {
    return exclusive(async () => {
      if (!online()) {
        set({ status: 'offline' });
        await recount();
        return;
      }
      if (!opts.force && state.nextAttemptAt !== null && now() < state.nextAttemptAt) return;
      set({ status: 'syncing', nextAttemptAt: null });
      try {
        // A batch of 50 per round; the loop ends when nothing is left or a failure sets a backoff.
        for (let guard = 0; guard < 1000; guard += 1) {
          if ((await sendBatch()) === 'done') break;
        }
        if (state.status !== 'backoff') {
          const entries = await storage.listEntries();
          if (!entries.some((e) => e.state === 'pending' || e.state === 'sending')) await refreshCatalogue();
        }
      } catch (e) {
        failure(e, state.attempts + 1);
      }
      await recount();
      if (state.status === 'syncing' || state.status === 'resync') set({ status: 'idle' });
    });
  }

  /**
   * The online sale path (POST pos/sales): immediate server pricing and a receipt link. A network
   * failure leaves the sale in the outbox for `flush`; the same client id keeps the server from
   * recording it twice.
   */
  function sendSaleNow(clientId: string): Promise<OutboxEntry | undefined> {
    return exclusive(async () => {
      const entry = (await storage.listEntries()).find((e) => e.clientId === clientId);
      if (!entry || entry.kind !== 'sale' || entry.state !== 'pending') return entry;
      if (!online()) return entry;
      const sending: OutboxEntry = { ...entry, state: 'sending', attempts: entry.attempts + 1 };
      await storage.putEntry(sending);
      try {
        const ack = await api.createSale(saleToWire(entry.payload as Sale), clientId);
        await markApplied(sending, ack);
        await noteServerTime(ack.serverNow);
      } catch (e) {
        if (isRetryable(e)) {
          await storage.putEntry({ ...sending, state: 'pending', lastError: e instanceof ApiError ? e.code : 'network' });
        } else {
          await markRejected(sending, e instanceof ApiError ? e.code : 'rejected');
        }
      }
      await recount();
      return (await storage.listEntries()).find((x) => x.clientId === clientId);
    });
  }

  /** Removes a refused entry once a person has seen it. */
  async function dismiss(clientId: string) {
    await storage.removeEntries([clientId]);
    await recount();
  }

  /** Puts a refused entry back in the queue (the person fixed the cause, e.g. a permission). */
  async function retry(clientId: string) {
    const e = (await storage.listEntries()).find((x) => x.clientId === clientId);
    if (e?.state === 'rejected') await storage.putEntry({ ...e, state: 'pending', lastError: null, result: undefined });
    await recount();
  }

  return {
    state: () => state,
    subscribe(fn: (s: SyncState) => void) {
      listeners.add(fn);
      return () => void listeners.delete(fn);
    },
    nextAttemptAt: () => state.nextAttemptAt,
    recordSale,
    recordMovement,
    sendSaleNow,
    flush,
    refreshCatalogue: () => exclusive(refreshCatalogue),
    entries: () => storage.listEntries(),
    clockOffsetMs,
    dismiss,
    retry,
    recount,
    storage,
  };
}

export type SyncEngine = ReturnType<typeof createSyncEngine>;
