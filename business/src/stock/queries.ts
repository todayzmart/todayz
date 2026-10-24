import { ApiError } from '@todayz/api-client';
import {
  createStockMovement,
  createStoreItem,
  getGetStoreItemUrl,
  getStoreItem,
  getStoreItemHistory,
  listCategories,
  listStockBatches,
  listStoreItems,
  searchProducts,
  updateStockBatch,
  updateStoreItem,
} from '@todayz/api-client/gen/catalogue/catalogue';
import type { MovementCreate } from '@todayz/api-client/gen/model/movementCreate';
import type { MovementResult } from '@todayz/api-client/gen/model/movementResult';
import type { StockBatch } from '@todayz/api-client/gen/model/stockBatch';
import type { StockBatchUpdate } from '@todayz/api-client/gen/model/stockBatchUpdate';
import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import type { StoreItemCreate } from '@todayz/api-client/gen/model/storeItemCreate';
import type { StoreItemList } from '@todayz/api-client/gen/model/storeItemList';
import type { StoreItemUpdate } from '@todayz/api-client/gen/model/storeItemUpdate';
import type { Session } from '@todayz/auth';
import { useInfiniteQuery, useQueries, useQuery, type InfiniteData, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';

export const stockKey = (sid: string) => ['stock', sid] as const;
export const itemsKey = (sid: string, f: ItemFilters) => ['stock', sid, 'items', f] as const;
export const itemKey = (sid: string, id: string) => ['stock', sid, 'item', id] as const;
export const batchesKey = (sid: string, itemId: string) => ['stock', sid, 'batches', itemId] as const;
export const historyKey = (sid: string, itemId: string) => ['stock', sid, 'history', itemId] as const;

export interface ItemFilters {
  q?: string;
  categoryId?: string;
  lowStock?: boolean;
  listed?: boolean;
}

const PAGE = 50;

/** The shop's items, newest cursor page first; `fetchNextPage` loads more. */
export function useItems(store: CurrentStore, filters: ItemFilters) {
  return useInfiniteQuery({
    queryKey: itemsKey(store.sid, filters),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      listStoreItems(store.bid, store.sid, { ...filters, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last: StoreItemList) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

/** Items by id, for count mode and deep links. An id that is not in this store comes back as a 404 and is dropped. */
export function useItemsById(store: CurrentStore, ids: readonly string[]) {
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: itemKey(store.sid, id),
      queryFn: () => getStoreItem(store.bid, store.sid, id),
      retry: (n: number, err: unknown) => !(err instanceof ApiError && err.status < 500) && n < 1,
    })),
  });
}

export function useItem(store: CurrentStore, id: string | null) {
  return useQuery({
    queryKey: itemKey(store.sid, id ?? ''),
    queryFn: () => getStoreItem(store.bid, store.sid, id ?? ''),
    enabled: id !== null,
  });
}

export function useBatches(store: CurrentStore, itemId: string | null, enabled = true) {
  return useQuery({
    queryKey: batchesKey(store.sid, itemId ?? ''),
    queryFn: () => listStockBatches(store.bid, store.sid, { itemId: itemId ?? '' }),
    enabled: enabled && itemId !== null,
  });
}

export function useHistory(store: CurrentStore, itemId: string) {
  return useInfiniteQuery({
    queryKey: historyKey(store.sid, itemId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => getStoreItemHistory(store.bid, store.sid, itemId, { limit: 20, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useCategories() {
  return useQuery({ queryKey: ['stock', 'categories'], queryFn: () => listCategories(), staleTime: 10 * 60_000 });
}

export function useProductSearch(store: CurrentStore, q: string, barcode: string | undefined) {
  const on = (q.trim().length >= 2 && !barcode) || (barcode !== undefined && barcode.length > 0);
  return useQuery({
    queryKey: ['stock', store.sid, 'products', q.trim(), barcode ?? ''],
    queryFn: () => searchProducts(store.bid, { ...(barcode ? { barcode } : { q: q.trim() }), storeId: store.sid, limit: 20 }),
    enabled: on,
  });
}

/** Puts a changed item into every cache that shows it, then refetches what depends on it. */
export function applyItem(qc: QueryClient, sid: string, item: StoreItem): void {
  qc.setQueryData(itemKey(sid, item.id), item);
  qc.setQueriesData<InfiniteData<StoreItemList>>({ queryKey: [...stockKey(sid), 'items'] }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.map((i) => (i.id === item.id ? item : i)) })) } : old,
  );
  void qc.invalidateQueries({ queryKey: batchesKey(sid, item.id) });
  void qc.invalidateQueries({ queryKey: historyKey(sid, item.id) });
}

/** Restock, count, damage or expire (Idempotency-Key required). */
export async function sendMovement(qc: QueryClient, s: CurrentStore, itemId: string, body: MovementCreate, key: string): Promise<MovementResult> {
  const out = await createStockMovement(s.bid, s.sid, itemId, body, { idempotencyKey: key });
  applyItem(qc, s.sid, out.item);
  return out;
}

/**
 * Changes an item. The PATCH needs `If-Match`, and the item body has no version, so the tag comes
 * from a fresh GET of the same path (`session.etag`); a movement or another device may have changed
 * the item since the list was loaded. A 412 still means it changed again in between: the caller shows
 * "changed, try again" after the item is reloaded here.
 */
export async function patchItem(qc: QueryClient, session: Session, s: CurrentStore, itemId: string, body: StoreItemUpdate): Promise<StoreItem> {
  await getStoreItem(s.bid, s.sid, itemId);
  const tag = session.etag(getGetStoreItemUrl(s.bid, s.sid, itemId));
  try {
    const item = await updateStoreItem(s.bid, s.sid, itemId, body, tag ? { ifMatch: tag } : {});
    applyItem(qc, s.sid, item);
    return item;
  } catch (err) {
    if (err instanceof ApiError && err.status === 412) void qc.invalidateQueries({ queryKey: stockKey(s.sid) });
    throw err;
  }
}

/** Cost and expiry of one batch; the batch's own `etag` is the `If-Match`. */
export async function patchBatch(qc: QueryClient, s: CurrentStore, batch: StockBatch, body: StockBatchUpdate): Promise<StockBatch> {
  try {
    const out = await updateStockBatch(s.bid, s.sid, batch.id, body, { ifMatch: batch.etag });
    void qc.invalidateQueries({ queryKey: batchesKey(s.sid, batch.itemId) });
    return out;
  } catch (err) {
    if (err instanceof ApiError && err.status === 412) void qc.invalidateQueries({ queryKey: batchesKey(s.sid, batch.itemId) });
    throw err;
  }
}

export async function addItem(qc: QueryClient, s: CurrentStore, body: StoreItemCreate, key: string): Promise<StoreItem> {
  const item = await createStoreItem(s.bid, s.sid, body, { idempotencyKey: key });
  qc.setQueryData(itemKey(s.sid, item.id), item);
  void qc.invalidateQueries({ queryKey: [...stockKey(s.sid), 'items'] });
  return item;
}
