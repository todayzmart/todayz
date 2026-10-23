// C-09 cart persistence (01 §2: the cart survives reloads and sign-in). One IndexedDB database with
// one object store and one record; no library. Where IndexedDB is missing or blocked (a private
// window, tests) the cart still works for the life of the page.
import type { CartState } from './cart';

export interface CartStorage {
  load(): Promise<CartState | null>;
  save(state: CartState): Promise<void>;
}

const DB_NAME = 'todayz-customer';
const STORE = 'cart';
const KEY = 'current';
const VERSION = 1;

function open(factory: IDBFactory): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = factory.open(DB_NAME, VERSION);
    req.onupgradeneeded = () => {
      if (!req.result.objectStoreNames.contains(STORE)) req.result.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('indexeddb open failed'));
    req.onblocked = () => reject(new Error('indexeddb blocked'));
  });
}

function done(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error ?? new Error('indexeddb write failed'));
    tx.onabort = () => reject(tx.error ?? new Error('indexeddb write aborted'));
  });
}

function isCartState(v: unknown): v is CartState {
  if (typeof v !== 'object' || v === null) return false;
  const s = v as { store?: unknown; lines?: unknown };
  return Array.isArray(s.lines) && (s.store === null || typeof s.store === 'object');
}

/** IndexedDB-backed storage, or null when the browser has none. */
export function indexedDbStorage(factory: IDBFactory | undefined = globalThis.indexedDB): CartStorage | null {
  if (!factory) return null;
  let db: Promise<IDBDatabase> | null = null;
  const get = () => (db ??= open(factory));
  return {
    async load() {
      const d = await get();
      const req = d.transaction(STORE, 'readonly').objectStore(STORE).get(KEY);
      const value = await new Promise<unknown>((resolve, reject) => {
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error ?? new Error('indexeddb read failed'));
      });
      return isCartState(value) ? value : null;
    },
    async save(state) {
      const d = await get();
      const tx = d.transaction(STORE, 'readwrite');
      if (state.lines.length === 0) tx.objectStore(STORE).delete(KEY);
      else tx.objectStore(STORE).put(JSON.parse(JSON.stringify(state)) as CartState, KEY);
      await done(tx);
    },
  };
}
