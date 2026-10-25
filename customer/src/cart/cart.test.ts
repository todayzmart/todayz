import { afterEach, describe, expect, it } from 'vitest';
import {
  addToCart,
  attachCartStorage,
  cartCount,
  cartSettled,
  clearCart,
  getCart,
  replaceCart,
  resetCartForTests,
  setQty,
  type CartState,
} from './cart';
import { indexedDbStorage, type CartStorage } from './storage';

const shopA = { id: 'A', slug: 'a', name: 'Shop A' };
const shopB = { id: 'B', slug: 'b', name: 'Shop B' };
const atta = { storeItemId: 'i1', productName: 'Atta', pricePaise: 5000 };
const dal = { storeItemId: 'i2', productName: 'Dal', pricePaise: 9000 };

/** A just-enough IndexedDB: one database, one object store, callbacks on a microtask. */
function fakeIdb(data: Map<string, unknown>): IDBFactory {
  const later = (fn: () => void) => void Promise.resolve().then(fn);
  const store = {
    get: (k: string) => {
      const req: { result?: unknown; onsuccess?: () => void } = {};
      later(() => {
        req.result = data.get(k);
        req.onsuccess?.();
      });
      return req;
    },
    put: (v: unknown, k: string) => void data.set(k, v),
    delete: (k: string) => void data.delete(k),
  };
  const db = {
    objectStoreNames: { contains: () => true },
    transaction: () => {
      const tx: { oncomplete?: () => void; objectStore: () => typeof store } = { objectStore: () => store };
      later(() => later(() => tx.oncomplete?.()));
      return tx;
    },
  };
  return {
    open: () => {
      const req: { result: unknown; onsuccess?: () => void } = { result: db };
      later(() => req.onsuccess?.());
      return req;
    },
  } as unknown as IDBFactory;
}

function memoryStorage(initial: CartState | null = null): CartStorage & { saved: CartState[] } {
  const saved: CartState[] = [];
  return {
    saved,
    load: () => Promise.resolve(initial),
    save: (s) => {
      saved.push(s);
      return Promise.resolve();
    },
  };
}

afterEach(() => resetCartForTests());

describe('cart', () => {
  it('keeps the interface: add, merge, set, replace, clear, count', () => {
    expect(addToCart(shopA, atta)).toEqual({ ok: true });
    addToCart(shopA, atta, 2);
    addToCart(shopA, dal);
    expect(cartCount(getCart())).toBe(4);
    setQty('i2', 0);
    expect(getCart().lines).toHaveLength(1);
    expect(addToCart(shopB, dal)).toEqual({ ok: false, reason: 'other_store', current: shopA });
    replaceCart(shopB, dal, 2);
    expect(getCart().store).toEqual(shopB);
    clearCart();
    expect(getCart().store).toBeNull();
  });

  it('writes every change through to the storage and loads it back after a reload', async () => {
    const data = new Map<string, unknown>();
    const first = indexedDbStorage(fakeIdb(data));
    expect(first).not.toBeNull();
    await attachCartStorage(first);
    addToCart(shopA, atta, 2);
    await cartSettled();

    // "Reload": fresh memory, same database.
    resetCartForTests();
    expect(getCart().lines).toHaveLength(0);
    await attachCartStorage(indexedDbStorage(fakeIdb(data)));
    expect(getCart().store).toEqual(shopA);
    expect(getCart().lines).toEqual([{ ...atta, qty: 2 }]);

    clearCart();
    await cartSettled();
    expect(data.size).toBe(0);
  });

  it('a change made before the load finishes is kept and the same shop merges', async () => {
    const saved: CartState = { store: shopA, lines: [{ ...dal, qty: 1 }] };
    const storage = memoryStorage(saved);
    const loading = attachCartStorage(storage);
    addToCart(shopA, atta);
    await loading;
    expect(getCart().lines.map((l) => l.storeItemId).sort()).toEqual(['i1', 'i2']);
  });

  it('a saved cart from another shop does not overwrite a newer one', async () => {
    const storage = memoryStorage({ store: shopB, lines: [{ ...dal, qty: 1 }] });
    const loading = attachCartStorage(storage);
    addToCart(shopA, atta);
    await loading;
    expect(getCart().store).toEqual(shopA);
  });

  it('a loaded cart keeps a reply only within its asked quantity', async () => {
    await attachCartStorage(
      memoryStorage({
        store: shopA,
        lines: [
          { ...atta, qty: 2, askReplyId: 'R1', askQty: 2, askPriced: true },
          { ...dal, qty: 3, askReplyId: 'R2', askQty: 2, askPriced: true },
          { storeItemId: 'i3', productName: 'Salt', pricePaise: 2000, qty: 1, askReplyId: 'R3' },
          { storeItemId: 'i4', productName: 'Rice', pricePaise: 7000, qty: 1, askReplyId: 'R4', askQty: 0 },
        ],
      }),
    );
    const [kept, ...dropped] = getCart().lines;
    expect(kept).toMatchObject({ askReplyId: 'R1', askQty: 2, askPriced: true });
    expect(dropped).toHaveLength(3);
    for (const l of dropped) {
      expect(l).not.toHaveProperty('askReplyId');
      expect(l).not.toHaveProperty('askQty');
      expect(l).not.toHaveProperty('askPriced');
    }
    expect(dropped.map((l) => l.qty)).toEqual([3, 1, 1]);
  });

  it('ignores damaged records and works without IndexedDB', async () => {
    await attachCartStorage(memoryStorage({ store: shopA, lines: [{ storeItemId: 'x', qty: 'many' }] } as unknown as CartState));
    expect(getCart().lines).toHaveLength(0);
    expect(indexedDbStorage(undefined)).toBeNull();
    await attachCartStorage(null);
    addToCart(shopA, atta);
    expect(cartCount(getCart())).toBe(1);
  });
});
