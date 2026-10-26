import {
  approveAdminPendingProduct,
  approveAdminProductImage,
  approveAdminProductImages,
  confirmAdminProductImage,
  createAdminProductImage,
  createAdminCategory,
  createAdminProduct,
  createAdminSynonym,
  deleteAdminSynonym,
  getAdminProduct,
  listAdminCategories,
  listAdminPendingProducts,
  listAdminProductImages,
  listAdminProducts,
  listAdminSynonyms,
  mergeAdminPendingProduct,
  rejectAdminPendingProduct,
  rejectAdminProductImage,
  updateAdminCategory,
  updateAdminProduct,
} from '@todayz/api-client/gen/admin-catalogue/admin-catalogue';
import type { AdminCategoryCreate } from '@todayz/api-client/gen/model/adminCategoryCreate';
import type { AdminCategoryUpdate } from '@todayz/api-client/gen/model/adminCategoryUpdate';
import type { AdminProductCreate } from '@todayz/api-client/gen/model/adminProductCreate';
import type { AdminProductImageBulkResult } from '@todayz/api-client/gen/model/adminProductImageBulkResult';
import { AdminProductImageContentType } from '@todayz/api-client/gen/model/adminProductImageContentType';
import type { AdminProductImageSource } from '@todayz/api-client/gen/model/adminProductImageSource';
import type { AdminProductUpdate } from '@todayz/api-client/gen/model/adminProductUpdate';
import type { AdminSynonymCreate } from '@todayz/api-client/gen/model/adminSynonymCreate';
import type { ListAdminProductsParams } from '@todayz/api-client/gen/model/listAdminProductsParams';
import { ApiError } from '@todayz/api-client';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

type ProductFilter = Omit<ListAdminProductsParams, 'limit' | 'cursor'>;

export const productsKey = (f: ProductFilter) => ['admin', 'products', f] as const;
export const productKey = (id: string) => ['admin', 'product', id] as const;
export const pendingKey = ['admin', 'pending-products'] as const;
export const categoriesKey = ['admin', 'categories'] as const;
export const synonymsKey = (q: string) => ['admin', 'synonyms', q] as const;
export const imagesKey = (source: AdminProductImageSource | undefined) => ['admin', 'product-images', source ?? ''] as const;

export function useProducts(f: ProductFilter) {
  return useInfiniteQuery({
    queryKey: productsKey(f),
    queryFn: ({ pageParam }) => listAdminProducts({ ...f, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** One product, for a side panel opened from a link when the row is not on the loaded pages. */
export function useProduct(id: string | undefined, enabled: boolean) {
  return useQuery({ queryKey: productKey(id ?? ''), queryFn: () => getAdminProduct(id ?? ''), enabled: enabled && !!id });
}

export function usePending() {
  return useInfiniteQuery({
    queryKey: pendingKey,
    queryFn: ({ pageParam }) => listAdminPendingProducts({ limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

/** Every category: a small reference list, so it is one call and cached for the session's screens. */
export function useCategories() {
  return useQuery({ queryKey: categoriesKey, queryFn: () => listAdminCategories(), staleTime: 60_000 });
}

export function useSynonyms(q: string) {
  return useInfiniteQuery({
    queryKey: synonymsKey(q),
    queryFn: ({ pageParam }) => listAdminSynonyms({ ...(q ? { q } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

function refreshProducts(qc: QueryClient, id?: string) {
  void qc.invalidateQueries({ queryKey: ['admin', 'products'] });
  void qc.invalidateQueries({ queryKey: pendingKey });
  if (id) {
    void qc.invalidateQueries({ queryKey: productKey(id) });
    refreshAudit(qc, 'product', id);
  }
}

/**
 * Every catalogue change below is not idempotency-keyed: a repeated create is `409 conflict` (the
 * barcode, slug or pair is taken) and a repeated decision is `409 invalid_transition`.
 */
export function useCreateProduct() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminProductCreate) => createAdminProduct(body),
    onSuccess: (p) => refreshProducts(qc, p.id),
  });
}

export function useUpdateProduct(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminProductUpdate) => updateAdminProduct(id, body),
    onSettled: () => refreshProducts(qc, id),
  });
}

/** A decision on a pending product; approve and merge take an optional note, reject a reason. */
export type Decision = { kind: 'approve'; reason?: string } | { kind: 'reject'; reason: string } | { kind: 'merge'; into: string; reason?: string };

const note = (reason?: string) => (reason ? { reason } : {});

/** Approve, merge into an active product, or reject a pending product; each audited on the product. */
export function useDecide(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (d: Decision) => {
      switch (d.kind) {
        case 'approve':
          return approveAdminPendingProduct(id, note(d.reason));
        case 'reject':
          return rejectAdminPendingProduct(id, { reason: d.reason });
        case 'merge':
          return mergeAdminPendingProduct(id, { intoProductId: d.into, ...note(d.reason) });
      }
    },
    onSettled: (_p, _e, d) => {
      refreshProducts(qc, id);
      if (d.kind === 'merge') refreshProducts(qc, d.into);
    },
  });
}

function refreshCategories(qc: QueryClient, id?: string) {
  void qc.invalidateQueries({ queryKey: categoriesKey });
  if (id) refreshAudit(qc, 'category', id);
}

export function useCreateCategory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminCategoryCreate) => createAdminCategory(body),
    onSuccess: (c) => refreshCategories(qc, c.id),
  });
}

export function useUpdateCategory(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminCategoryUpdate) => updateAdminCategory(id, body),
    onSettled: () => refreshCategories(qc, id),
  });
}

export function useCreateSynonym() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AdminSynonymCreate) => createAdminSynonym(body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'synonyms'] }),
  });
}

export function useDeleteSynonym() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteAdminSynonym(id),
    onSettled: (_d, _e, id) => {
      void qc.invalidateQueries({ queryKey: ['admin', 'synonyms'] });
      refreshAudit(qc, 'synonym', id);
    },
  });
}

/** Images waiting for a decision (D-252): shop photos and imported drafts, oldest first. */
export function useImages(source: AdminProductImageSource | undefined) {
  return useInfiniteQuery({
    queryKey: imagesKey(source),
    queryFn: ({ pageParam }) =>
      listAdminProductImages({ status: 'pending', ...(source ? { source } : {}), limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextCursor ?? undefined,
  });
}

function refreshImages(qc: QueryClient, productIds: readonly string[]) {
  void qc.invalidateQueries({ queryKey: ['admin', 'product-images'] });
  void qc.invalidateQueries({ queryKey: ['admin', 'products'] });
  for (const id of new Set(productIds)) {
    void qc.invalidateQueries({ queryKey: productKey(id) });
    refreshAudit(qc, 'product', id);
  }
}

export type ImageDecision = { kind: 'approve'; reason?: string } | { kind: 'reject'; reason: string };

/** Approve (the image becomes the product's and goes to the image host) or reject one waiting image. */
export function useDecideImage(id: string, productId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (d: ImageDecision) => (d.kind === 'approve' ? approveAdminProductImage(id, note(d.reason)) : rejectAdminProductImage(id, { reason: d.reason })),
    onSettled: () => refreshImages(qc, [productId]),
  });
}

/** The contract's bound on one bulk approval. */
export const BULK_MAX = 50;

/** Approves the chosen images, BULK_MAX at a time; each one stands or fails on its own. */
export function useApproveImages() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ ids, reason }: { ids: readonly string[]; reason?: string }) => {
      const out: AdminProductImageBulkResult = { approved: [], failed: [] };
      for (let i = 0; i < ids.length; i += BULK_MAX) {
        const r = await approveAdminProductImages({ imageIds: ids.slice(i, i + BULK_MAX), ...note(reason) });
        out.approved.push(...r.approved);
        out.failed.push(...r.failed);
      }
      return out;
    },
    onSettled: (r) => refreshImages(qc, r?.approved.map((i) => i.productId) ?? []),
  });
}

export const IMAGE_TYPES: readonly string[] = Object.values(AdminProductImageContentType);
/** The contract's bound on an uploaded image (AdminProductImageCreate.sizeBytes). */
export const IMAGE_MAX_BYTES = 5 * 1024 * 1024;

/** The file did not reach storage, or storage does not hold what was registered. */
export class UploadError extends Error {}

/**
 * An admin's image for a product: registered, PUT to the presigned URL with only Content-Type,
 * then confirmed, which publishes it at once and replaces the current image.
 */
export function useUploadImage(productId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (file: File) => {
      const made = await createAdminProductImage(productId, { contentType: file.type as AdminProductImageContentType, sizeBytes: file.size });
      const res = await fetch(made.upload.url, {
        method: made.upload.method,
        headers: { 'Content-Type': made.upload.headers['Content-Type'] },
        body: file,
      });
      if (!res.ok) throw new UploadError(String(res.status));
      return confirmAdminProductImage(made.image.id, {}).catch((err: unknown) => {
        // 422 on `upload`: the file is not in storage, or not what was registered.
        throw err instanceof ApiError && err.status === 422 ? new UploadError(String(err.status)) : err;
      });
    },
    onSettled: () => refreshImages(qc, [productId]),
  });
}
