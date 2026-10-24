// Where the POS keeps its data on the device. One interface, two implementations: IndexedDB in the
// browser (`idb.ts`) and memory for tests and for a browser with IndexedDB blocked. The catalogue
// snapshot, the outbox and a few meta values live in separate object stores.
import type { CatalogueItem } from '../pos/types';
import type { CatalogueMeta, ClockCheck, Inflight, OutboxEntry } from './types';

export interface PosStorage {
  loadCatalogue(): Promise<{ items: CatalogueItem[]; meta: CatalogueMeta | null }>;
  /** Replaces the whole catalogue (a snapshot). */
  saveCatalogue(items: CatalogueItem[], meta: CatalogueMeta): Promise<void>;
  /** Applies a delta in one transaction and moves the cursor. */
  applyChanges(upserts: CatalogueItem[], deletes: string[], meta: CatalogueMeta): Promise<void>;
  /** Next device sequence number; atomic across tabs. */
  nextSeq(): Promise<number>;
  putEntry(entry: OutboxEntry): Promise<void>;
  listEntries(): Promise<OutboxEntry[]>;
  removeEntries(clientIds: string[]): Promise<void>;
  getInflight(): Promise<Inflight | null>;
  setInflight(v: Inflight | null): Promise<void>;
  getClockCheck(): Promise<ClockCheck | null>;
  setClockCheck(v: ClockCheck): Promise<void>;
}

/** In-memory storage: used by tests and as the fallback when IndexedDB is unavailable. */
export function memoryStorage(): PosStorage & { dump(): { entries: OutboxEntry[]; seq: number } } {
  let items = new Map<string, CatalogueItem>();
  let meta: CatalogueMeta | null = null;
  const entries = new Map<string, OutboxEntry>();
  let seq = 0;
  let inflight: Inflight | null = null;
  let clock: ClockCheck | null = null;
  const sorted = () => [...entries.values()].sort((a, b) => a.seq - b.seq).map((e) => structuredClone(e));
  return {
    loadCatalogue: () => Promise.resolve({ items: structuredClone([...items.values()]), meta: structuredClone(meta) }),
    saveCatalogue: (next, m) => {
      items = new Map(next.map((i) => [i.id, structuredClone(i)]));
      meta = structuredClone(m);
      return Promise.resolve();
    },
    applyChanges: (upserts, deletes, m) => {
      for (const i of upserts) items.set(i.id, structuredClone(i));
      for (const id of deletes) items.delete(id);
      meta = structuredClone(m);
      return Promise.resolve();
    },
    nextSeq: () => Promise.resolve(++seq),
    putEntry: (e) => {
      entries.set(e.clientId, structuredClone(e));
      return Promise.resolve();
    },
    listEntries: () => Promise.resolve(sorted()),
    removeEntries: (ids) => {
      for (const id of ids) entries.delete(id);
      return Promise.resolve();
    },
    getInflight: () => Promise.resolve(structuredClone(inflight)),
    setInflight: (v) => {
      inflight = structuredClone(v);
      return Promise.resolve();
    },
    getClockCheck: () => Promise.resolve(structuredClone(clock)),
    setClockCheck: (v) => {
      clock = structuredClone(v);
      return Promise.resolve();
    },
    dump: () => ({ entries: sorted(), seq }),
  };
}
