import {
  approveAdminPendingProduct,
  createAdminCategory,
  createAdminProduct,
  createAdminSynonym,
  deleteAdminSynonym,
  getAdminProduct,
  listAdminCategories,
  listAdminPendingProducts,
  listAdminProducts,
  listAdminSynonyms,
  mergeAdminPendingProduct,
  rejectAdminPendingProduct,
  updateAdminCategory,
  updateAdminProduct,
} from '@todayz/api-client/gen/admin-catalogue/admin-catalogue';
import type { AdminCategoryCreate } from '@todayz/api-client/gen/model/adminCategoryCreate';
import type { AdminCategoryUpdate } from '@todayz/api-client/gen/model/adminCategoryUpdate';
import type { AdminProductCreate } from '@todayz/api-client/gen/model/adminProductCreate';
import type { AdminProductUpdate } from '@todayz/api-client/gen/model/adminProductUpdate';
import type { AdminSynonymCreate } from '@todayz/api-client/gen/model/adminSynonymCreate';
import type { ListAdminProductsParams } from '@todayz/api-client/gen/model/listAdminProductsParams';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { PAGE, refreshAudit } from '../audit/queries';

type ProductFilter = Omit<ListAdminProductsParams, 'limit' | 'cursor'>;

export const productsKey = (f: ProductFilter) => ['admin', 'products', f] as const;
export const productKey = (id: string) => ['admin', 'product', id] as const;
export const pendingKey = ['admin', 'pending-products'] as const;
export const categoriesKey = ['admin', 'categories'] as const;
export const synonymsKey = (q: string) => ['admin', 'synonyms', q] as const;

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
