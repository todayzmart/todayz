import {
  confirmFieldDocument,
  createFieldBusiness,
  createFieldDocument,
  createFieldItem,
  createFieldStore,
  getFieldBusiness,
  getFieldMyStats,
  listFieldAgents,
  listFieldBusinesses,
  listFieldItems,
  searchFieldProducts,
  sendFieldOwnerCode,
  updateFieldBusiness,
  updateFieldItem,
  verifyFieldOwnerCode,
} from '@todayz/api-client/gen/admin-field/admin-field';
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldBusinessCreate } from '@todayz/api-client/gen/model/fieldBusinessCreate';
import type { FieldBusinessUpdate } from '@todayz/api-client/gen/model/fieldBusinessUpdate';
import type { FieldDocumentKind } from '@todayz/api-client/gen/model/fieldDocumentKind';
import type { FieldItemCreate } from '@todayz/api-client/gen/model/fieldItemCreate';
import type { FieldItemUpdate } from '@todayz/api-client/gen/model/fieldItemUpdate';
import type { FieldPhotoContentType } from '@todayz/api-client/gen/model/fieldPhotoContentType';
import type { FieldStoreCreate } from '@todayz/api-client/gen/model/fieldStoreCreate';
import { ApiError } from '@todayz/api-client';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useIdempotent } from '../app/idempotency';
import { PAGE } from '../audit/queries';

export const fieldBusinessKey = (id: string) => ['admin', 'field', 'business', id] as const;
const itemsKey = (bid: string, sid: string) => ['admin', 'field', 'items', bid, sid] as const;
const LIST = ['admin', 'field', 'businesses'] as const;
const MY_STATS = ['admin', 'field', 'me', 'stats'] as const;

/** The caller's shops (an agent sees only the ones they onboarded), newest first. */
export function useFieldBusinesses() {
  return useInfiniteQuery({
    queryKey: LIST,
    queryFn: ({ pageParam }) => listFieldBusinesses({ limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

export function useFieldBusiness(id: string) {
  return useQuery({ queryKey: fieldBusinessKey(id), queryFn: () => getFieldBusiness(id) });
}

/** Top-level categories name the business; their children file a new product. Public and cached. */
export function useCategories() {
  return useQuery({ queryKey: ['catalogue', 'categories'], queryFn: () => listCategories(), staleTime: 60 * 60_000 });
}

export function useCreateBusiness() {
  const qc = useQueryClient();
  const once = useIdempotent();
  return useMutation({
    mutationFn: (body: FieldBusinessCreate) => once((headers) => createFieldBusiness(body, { headers })),
    onSuccess: (b) => {
      qc.setQueryData(fieldBusinessKey(b.id), b);
      void qc.invalidateQueries({ queryKey: LIST });
      void qc.invalidateQueries({ queryKey: MY_STATS });
    },
  });
}

/** Every change to one shop answers (or is followed by) the shop as it is now. */
function useShopChange<A, R>(bid: string, call: (a: A) => Promise<R>, after?: (r: R) => FieldBusiness | undefined) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: call,
    onSuccess: (r) => {
      const b = after?.(r);
      if (b) qc.setQueryData(fieldBusinessKey(bid), b);
      else void qc.invalidateQueries({ queryKey: fieldBusinessKey(bid) });
      void qc.invalidateQueries({ queryKey: LIST });
      void qc.invalidateQueries({ queryKey: MY_STATS });
    },
  });
}

export function useUpdateBusiness(bid: string) {
  return useShopChange(bid, (body: FieldBusinessUpdate) => updateFieldBusiness(bid, body), (b) => b);
}

export function useCreateStore(bid: string) {
  const once = useIdempotent();
  return useShopChange(bid, (body: FieldStoreCreate) => once((headers) => createFieldStore(bid, body, { headers })));
}

export class UploadError extends Error {}

export const PHOTO_TYPES: readonly FieldPhotoContentType[] = ['image/jpeg', 'image/png', 'image/webp'];

/**
 * Registers the photo, PUTs the file to the presigned URL the API answered, then confirms it: the
 * photo counts only once the API has seen the stored file.
 */
export function useAddPhoto(bid: string) {
  return useShopChange(bid, async ({ file, kind, storeId }: { file: File; kind: FieldDocumentKind; storeId?: string | undefined }) => {
    const made = await createFieldDocument(bid, {
      kind,
      contentType: file.type as FieldPhotoContentType,
      sizeBytes: file.size,
      ...(storeId ? { storeId } : {}),
    });
    const res = await fetch(made.upload.url, {
      method: made.upload.method,
      headers: { 'Content-Type': made.upload.headers['Content-Type'] },
      body: file,
    });
    if (!res.ok) throw new UploadError();
    return confirmFieldDocument(bid, made.document.id).catch((err: unknown) => {
      // 422: the file is not in storage, or not what was registered; 409: the URL expired.
      throw err instanceof ApiError && (err.status === 422 || err.status === 409) ? new UploadError() : err;
    });
  });
}

export function useSendCode(bid: string) {
  return useMutation({ mutationFn: () => sendFieldOwnerCode(bid) });
}

export function useVerifyCode(bid: string) {
  return useShopChange(bid, (code: string) => verifyFieldOwnerCode(bid, { code }), (b) => b);
}

export function useItems(bid: string, sid: string) {
  return useInfiniteQuery({
    queryKey: itemsKey(bid, sid),
    queryFn: ({ pageParam }) => listFieldItems(bid, sid, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** Master products matching at least 2 characters, or a barcode. */
export function useProductSearch(bid: string, sid: string, q: string) {
  const term = q.trim();
  const barcode = /^[0-9]{8,14}$/.test(term);
  return useQuery({
    queryKey: ['admin', 'field', 'products', bid, sid, term],
    queryFn: () => searchFieldProducts(bid, sid, barcode ? { barcode: term } : { q: term }),
    enabled: term.length >= 2,
    staleTime: 30_000,
  });
}

function useItemChange<A, R>(bid: string, sid: string, call: (a: A) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: call,
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: itemsKey(bid, sid) });
      void qc.invalidateQueries({ queryKey: ['admin', 'field', 'products', bid, sid] });
      void qc.invalidateQueries({ queryKey: fieldBusinessKey(bid) });
      void qc.invalidateQueries({ queryKey: MY_STATS });
    },
  });
}

export function useAddItem(bid: string, sid: string) {
  const once = useIdempotent();
  return useItemChange(bid, sid, (body: FieldItemCreate) => once((headers) => createFieldItem(bid, sid, body, { headers })));
}

export function useUpdateItem(bid: string, sid: string) {
  return useItemChange(bid, sid, ({ id, body }: { id: string; body: FieldItemUpdate }) => updateFieldItem(bid, sid, id, body));
}

/** A-15 for a field agent: their own row and trend. */
export function useMyStats(enabled: boolean) {
  return useQuery({ queryKey: MY_STATS, queryFn: () => getFieldMyStats(), enabled });
}

/** A-15 for admins: every field agent. */
export function useAgents(enabled: boolean) {
  return useQuery({ queryKey: ['admin', 'field', 'agents'], queryFn: () => listFieldAgents(), enabled });
}
