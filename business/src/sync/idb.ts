// IndexedDB implementation of PosStorage, written against the browser API directly: Dexie is named in
// the plan (W2-11) but is not in the lockfile, and a new dependency needs the lead's say-so (D-88).
// The schema is the one Dexie would declare:
//   catalogue: keyPath id            outbox: keyPath clientId, index seq            meta: out-of-line keys
// Version bumps go through `onupgradeneeded` below; never edit a released version in place.
import type { CatalogueItem } from '../pos/types';
import { memoryStorage, type PosStorage } from './storage';
import type { CatalogueMeta, ClockCheck, Inflight, OutboxEntry } from './types';

const VERSION = 1;

function wrap<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('indexeddb request failed'));
  });
}

function done(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error ?? new Error('indexeddb transaction failed'));
    tx.onabort = () => reject(tx.error ?? new Error('indexeddb transaction aborted'));
  });
}

export function openPosDb(name: string, factory: IDBFactory = indexedDB): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = factory.open(name, VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains('catalogue')) db.createObjectStore('catalogue', { keyPath: 'id' });
      if (!db.objectStoreNames.contains('outbox')) {
        db.createObjectStore('outbox', { keyPath: 'clientId' }).createIndex('seq', 'seq', { unique: true });
      }
      if (!db.objectStoreNames.contains('meta')) db.createObjectStore('meta');
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('indexeddb open failed'));
    req.onblocked = () => reject(new Error('indexeddb open blocked'));
  });
}

/** One database per store on this device (`todayz-pos-<storeId>`), so a shared tablet never mixes shops. */
export function idbStorage(db: IDBDatabase): PosStorage {
  const meta = async <T>(key: string): Promise<T | null> => {
    const v = (await wrap(db.transaction('meta').objectStore('meta').get(key))) as T | undefined;
    return v ?? null;
  };
  const setMeta = async (key: string, value: unknown) => {
    const tx = db.transaction('meta', 'readwrite');
    if (value === null) tx.objectStore('meta').delete(key);
    else tx.objectStore('meta').put(value, key);
    await done(tx);
  };
  return {
    async loadCatalogue() {
      const items = (await wrap(db.transaction('catalogue').objectStore('catalogue').getAll())) as CatalogueItem[];
      return { items, meta: await meta<CatalogueMeta>('catalogueMeta') };
    },
    async saveCatalogue(items, m) {
      const tx = db.transaction(['catalogue', 'meta'], 'readwrite');
      const store = tx.objectStore('catalogue');
      store.clear();
      for (const i of items) store.put(i);
      tx.objectStore('meta').put(m, 'catalogueMeta');
      await done(tx);
    },
    async applyChanges(upserts, deletes, m) {
      const tx = db.transaction(['catalogue', 'meta'], 'readwrite');
      const store = tx.objectStore('catalogue');
      for (const i of upserts) store.put(i);
      for (const id of deletes) store.delete(id);
      tx.objectStore('meta').put(m, 'catalogueMeta');
      await done(tx);
    },
    async nextSeq() {
      // One readwrite transaction: two tabs cannot take the same number.
      const tx = db.transaction('meta', 'readwrite');
      const store = tx.objectStore('meta');
      const current = ((await wrap(store.get('seq'))) as number | undefined) ?? 0;
      const next = current + 1;
      store.put(next, 'seq');
      await done(tx);
      return next;
    },
    async putEntry(e) {
      const tx = db.transaction('outbox', 'readwrite');
      tx.objectStore('outbox').put(e);
      await done(tx);
    },
    async listEntries() {
      return (await wrap(db.transaction('outbox').objectStore('outbox').index('seq').getAll())) as OutboxEntry[];
    },
    async removeEntries(ids) {
      const tx = db.transaction('outbox', 'readwrite');
      for (const id of ids) tx.objectStore('outbox').delete(id);
      await done(tx);
    },
    getInflight: () => meta<Inflight>('inflight'),
    setInflight: (v) => setMeta('inflight', v),
    getClockCheck: () => meta<ClockCheck>('clock'),
    setClockCheck: (v) => setMeta('clock', v),
  };
}

/** IndexedDB when the browser lets us, memory otherwise (a private window, blocked site data). */
export async function openStorage(storeId: string): Promise<{ storage: PosStorage; durable: boolean }> {
  try {
    if (typeof indexedDB === 'undefined') throw new Error('no indexeddb');
    const db = await openPosDb(`todayz-pos-${storeId}`);
    return { storage: idbStorage(db), durable: true };
  } catch {
    return { storage: memoryStorage(), durable: false };
  }
}
