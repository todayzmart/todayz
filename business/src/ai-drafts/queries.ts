import { ApiError } from '@todayz/api-client';
import { getStoreAiRetailFeatures } from '@todayz/api-client/gen/ai/ai';
import type { AiDraftStatus } from '@todayz/api-client/gen/model/aiDraftStatus';
import type { AiRetailFeatures } from '@todayz/api-client/gen/model/aiRetailFeatures';
import { useQuery } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';

/**
 * How often a draft still `processing` is read again. A client cadence, not a product rule: the
 * server closes a draft that outlives the provider timeout as failed, so polling always ends.
 */
export const POLL_MS = 1500;

export const aiKey = (s: CurrentStore) => ['biz', s.bid, 'store', s.sid, 'ai'] as const;

/** Every retailer AI feature off: what a store sees when the features call is a 404 or fails. */
export const NONE: AiRetailFeatures = { supplierBill: false, shelfPhoto: false, reviewReply: false, bannerCopy: false, maxPhotoBytes: 0, photoTypes: [] };

/** The store's retailer AI switches and photo limits. A failure counts as everything off: an AI entry point never shows broken. */
export function useAiFeatures(store: CurrentStore): AiRetailFeatures {
  const q = useQuery({
    queryKey: [...aiKey(store), 'features'],
    queryFn: () => getStoreAiRetailFeatures(store.bid, store.sid),
    retry: (n, err) => !(err instanceof ApiError) && n < 2,
    staleTime: 60_000,
  });
  return q.data ?? NONE;
}

/** One draft, read again while the model is still working on it. */
export function useDraft<T extends { status: AiDraftStatus }>(store: CurrentStore, feature: string, draftId: string | null, get: (id: string) => Promise<T>) {
  return useQuery({
    queryKey: [...aiKey(store), feature, draftId],
    queryFn: () => get(draftId ?? ''),
    enabled: draftId !== null,
    refetchInterval: (q) => (q.state.data?.status === 'processing' ? POLL_MS : false),
    refetchOnWindowFocus: false,
  });
}

/** The request options that carry a confirm's Idempotency-Key. */
export const withKey = (key: string) => ({ headers: { 'Idempotency-Key': key } });
