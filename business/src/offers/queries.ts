import { ApiError } from '@todayz/api-client';
import { createBanner, listBanners, updateBanner } from '@todayz/api-client/gen/banners/banners';
import { listStockBatches } from '@todayz/api-client/gen/catalogue/catalogue';
import { createOffer, listOffers, previewOffer, updateOffer } from '@todayz/api-client/gen/offers/offers';
import type { Banner } from '@todayz/api-client/gen/model/banner';
import type { BannerCreate } from '@todayz/api-client/gen/model/bannerCreate';
import type { BannerUpdateStatus } from '@todayz/api-client/gen/model/bannerUpdateStatus';
import type { Offer } from '@todayz/api-client/gen/model/offer';
import type { OfferCreate } from '@todayz/api-client/gen/model/offerCreate';
import type { OfferPreview } from '@todayz/api-client/gen/model/offerPreview';
import type { OfferUpdateStatus } from '@todayz/api-client/gen/model/offerUpdateStatus';
import type { SampleLine } from '@todayz/api-client/gen/model/sampleLine';
import { useInfiniteQuery, useQuery, type QueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import type { CurrentStore } from '../orders/store';
import type { ExpiringBatch } from './form';

const PAGE = 50;

export const offersKey = (sid: string) => ['offers', sid] as const;
export const offerListKey = (sid: string) => [...offersKey(sid), 'list'] as const;
export const bannerListKey = (sid: string) => [...offersKey(sid), 'banners'] as const;
export const expiringKey = (sid: string) => [...offersKey(sid), 'expiring'] as const;

/** The shop's offers, newest first. */
export function useOffers(store: CurrentStore) {
  return useInfiniteQuery({
    queryKey: offerListKey(store.sid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listOffers(store.bid, store.sid, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
  });
}

/** The shop's banners, newest first, with their impressions, taps and orders. */
export function useBanners(store: CurrentStore, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: bannerListKey(store.sid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => listBanners(store.bid, store.sid, { limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled,
    refetchOnMount: 'always',
  });
}

/**
 * Batches with stock left and an expiry date, soonest first, for the flash form. There is no
 * "expiring soon" window here on purpose: which batches count as close is the shop's call.
 */
export function useExpiringBatches(store: CurrentStore, enabled: boolean) {
  return useQuery({
    queryKey: expiringKey(store.sid),
    queryFn: async (): Promise<ExpiringBatch[]> => {
      const list = await listStockBatches(store.bid, store.sid, { limit: 100 });
      return list.items
        .filter((b) => b.quantity > 0 && !b.expired && b.expiresOn !== null)
        .map((b) => ({ id: b.id, itemId: b.itemId, itemName: b.itemName, expiresOn: b.expiresOn, quantity: b.quantity }))
        .sort((a, b) => (a.expiresOn ?? '').localeCompare(b.expiresOn ?? ''));
    },
    enabled,
  });
}

export interface CreateOffersResult {
  /** The bodies that did not go through (a flash form makes one offer per batch). */
  failed: OfferCreate[];
  error: unknown;
}

/**
 * Creates the offers, one request per body, each with its own Idempotency-Key (`keys[i]`). Stops at
 * the first failure and says which bodies are left, so a retry does not repeat the ones that worked.
 */
export async function createOffers(qc: QueryClient, s: CurrentStore, bodies: readonly OfferCreate[], keys: readonly string[]): Promise<CreateOffersResult> {
  for (let i = 0; i < bodies.length; i += 1) {
    const body = bodies[i];
    const key = keys[i];
    if (!body || !key) continue;
    try {
      await createOffer(s.bid, s.sid, body, { idempotencyKey: key });
    } catch (error) {
      void qc.invalidateQueries({ queryKey: offersKey(s.sid) });
      return { failed: bodies.slice(i), error };
    }
  }
  void qc.invalidateQueries({ queryKey: offersKey(s.sid) });
  return { failed: [], error: null };
}

/** Pauses, resumes or ends an offer. A 412 means the offer changed since it was loaded, so the list is reloaded. */
export async function setOfferStatus(qc: QueryClient, s: CurrentStore, offer: Offer, status: OfferUpdateStatus): Promise<Offer> {
  try {
    const out = await updateOffer(s.bid, s.sid, offer.id, { status }, { ifMatch: `"v${offer.version}"` });
    void qc.invalidateQueries({ queryKey: offerListKey(s.sid) });
    return out;
  } catch (err) {
    if (err instanceof ApiError && (err.status === 412 || err.status === 409 || err.status === 404)) void qc.invalidateQueries({ queryKey: offerListKey(s.sid) });
    throw err;
  }
}

export async function addBanner(qc: QueryClient, s: CurrentStore, body: BannerCreate, key: string): Promise<Banner> {
  try {
    return await createBanner(s.bid, s.sid, body, { idempotencyKey: key });
  } finally {
    void qc.invalidateQueries({ queryKey: bannerListKey(s.sid) });
  }
}

export async function setBannerStatus(qc: QueryClient, s: CurrentStore, banner: Banner, status: BannerUpdateStatus): Promise<Banner> {
  try {
    const out = await updateBanner(s.bid, s.sid, banner.id, { status }, { ifMatch: `"v${banner.version}"` });
    void qc.invalidateQueries({ queryKey: bannerListKey(s.sid) });
    return out;
  } catch (err) {
    if (err instanceof ApiError && (err.status === 412 || err.status === 409 || err.status === 404)) void qc.invalidateQueries({ queryKey: bannerListKey(s.sid) });
    throw err;
  }
}

const PREVIEW_DELAY_MS = 400;

/** The shop's price for a sample cart with the unsaved offer; asks only while `body` and `lines` are set, and waits for typing to pause. */
export function usePreview(store: CurrentStore, body: OfferCreate | null, lines: readonly SampleLine[]) {
  const signature = body && lines.length > 0 ? JSON.stringify([body, lines]) : '';
  const [settled, setSettled] = useState('');
  useEffect(() => {
    const t = setTimeout(() => setSettled(signature), signature ? PREVIEW_DELAY_MS : 0);
    return () => clearTimeout(t);
  }, [signature]);
  return useQuery({
    queryKey: [...offersKey(store.sid), 'preview', settled] as const,
    queryFn: (): Promise<OfferPreview> => {
      const [offer, sample] = JSON.parse(settled) as [OfferCreate, SampleLine[]];
      return previewOffer(store.bid, store.sid, { offer, lines: sample });
    },
    enabled: settled !== '' && settled === signature,
    staleTime: 60_000,
    retry: false,
  });
}

/** The i18n key suffix (`offers.error.<suffix>`) for a failed call; unknown codes are `generic`. */
export function offersErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'validation_failed':
      case 'forbidden':
      case 'not_found':
      case 'invalid_transition':
      case 'banner_limit_reached':
      case 'precondition_failed':
      case 'precondition_required':
      case 'conflict':
        return err.code;
    }
  }
  return 'generic';
}
