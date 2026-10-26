import { ApiError } from '@todayz/api-client';
import {
  getBusiness,
  getGetBusinessUrl,
  getGetKycUrl,
  getGetStoreSettingsUrl,
  getGetStoreUrl,
  getKyc,
  getStore,
  getStoreSettings,
  listBusinessDocuments,
  listBusinesses,
  listStaff,
  listStores,
} from '@todayz/api-client/gen/business/business';
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { newUlid, useSession, useSessionState } from '@todayz/auth';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useRef, useState } from 'react';
import { rememberedStore, type BusinessSummary } from '../app/shop';

/** The same key the shop picker and the orders desk use, so one `listBusinesses` answer serves all. */
export const businessesKey = ['businesses'] as const;
export const bizKey = (bid: string) => ['biz', bid] as const;
export const storesKey = (bid: string) => ['biz', bid, 'stores'] as const;
export const storeKey = (bid: string, sid: string) => ['biz', bid, 'store', sid] as const;
export const settingsKey = (bid: string, sid: string) => ['biz', bid, 'store', sid, 'settings'] as const;
export const staffKey = (bid: string) => ['biz', bid, 'staff'] as const;
export const kycKey = (bid: string) => ['biz', bid, 'kyc'] as const;
export const docsKey = (bid: string) => ['biz', bid, 'documents'] as const;

export function useBusinesses() {
  const { status } = useSessionState();
  return useQuery({ queryKey: businessesKey, queryFn: () => listBusinesses(), enabled: status === 'signedIn', retry: false });
}

export type CurrentBusiness =
  | { status: 'loading' }
  | { status: 'error'; retry: () => void }
  | { status: 'none' }
  | { status: 'ready'; business: BusinessSummary; invitations: number };

/**
 * The business this device works in: the one that holds the remembered store, else the only one,
 * else the first. A business still in onboarding has no store yet, so this cannot go through the
 * store picker.
 */
export function useCurrentBusiness(): CurrentBusiness {
  const q = useBusinesses();
  if (q.isError) return { status: 'error', retry: () => void q.refetch() };
  if (!q.data) return { status: 'loading' };
  const remembered = rememberedStore();
  const items = q.data.items;
  const business = items.find((b) => b.stores.some((s) => s.id === remembered)) ?? items[0];
  if (!business) return { status: 'none' };
  return { status: 'ready', business, invitations: q.data.invitations.length };
}

export const useBusiness = (bid: string, enabled = true) => useQuery({ queryKey: bizKey(bid), queryFn: () => getBusiness(bid), enabled });
export const useStores = (bid: string) => useQuery({ queryKey: storesKey(bid), queryFn: () => listStores(bid) });
export const useStore = (bid: string, sid: string) => useQuery({ queryKey: storeKey(bid, sid), queryFn: () => getStore(bid, sid) });
export const useStoreSettings = (bid: string, sid: string, enabled = true) =>
  useQuery({ queryKey: settingsKey(bid, sid), queryFn: () => getStoreSettings(bid, sid), enabled });
export const useStaff = (bid: string, enabled = true) => useQuery({ queryKey: staffKey(bid), queryFn: () => listStaff(bid), enabled });
export const useKyc = (bid: string, enabled = true) => useQuery({ queryKey: kycKey(bid), queryFn: () => getKyc(bid), enabled });
export const useDocuments = (bid: string, enabled = true) =>
  useQuery({ queryKey: docsKey(bid), queryFn: () => listBusinessDocuments(bid), enabled });
export const useCategories = () => useQuery({ queryKey: ['categories'], queryFn: () => listCategories(), staleTime: 3_600_000 });

/**
 * The `ETag` to send as `If-Match` on a PATCH (04 §1: mutable resources carry `"v{version}"`). The
 * session remembers the last ETag it saw for a path; when this device has not read the resource
 * yet, it is read first. A 412 means someone else changed it: the caller refetches and shows the
 * current values.
 */
export function useIfMatch() {
  const session = useSession();
  return useCallback(
    async (path: string, read: () => Promise<unknown>): Promise<string | undefined> => {
      const known = session.etag(path);
      if (known) return known;
      await read();
      return session.etag(path);
    },
    [session],
  );
}

export const businessPath = (bid: string) => getGetBusinessUrl(bid);
export const storePath = (bid: string, sid: string) => getGetStoreUrl(bid, sid);
export const settingsPath = (bid: string, sid: string) => getGetStoreSettingsUrl(bid, sid);
export const kycPath = (bid: string) => getGetKycUrl(bid);

/** The i18n key suffix (`stores.error.<key>`) for a failure; unknown codes fall back to `generic`. */
export function bizErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'forbidden':
      case 'conflict':
      case 'validation_failed':
      case 'submit_incomplete':
      case 'precondition_failed':
      case 'precondition_required':
      case 'invalid_transition':
      case 'service_unavailable':
      case 'not_found':
        return err.code;
    }
  }
  return 'generic';
}

/** The last path segment of a problem `fields` key (`stores[0].city` → `city`). */
export function fieldName(path: string): string {
  const last = path.split('.').pop() ?? path;
  return last.replace(/^body\./, '');
}

export interface Action {
  pending: boolean;
  error: unknown;
  /**
   * Runs one call. `idempotencyKey` is reused when the same named action is retried after a network
   * failure, and replaced after any answer from the server, so a lost request cannot act twice.
   */
  run<T>(name: string, call: (idempotencyKey: string) => Promise<T>): Promise<T | null>;
  clearError(): void;
}

export function useAction(): Action {
  const qc = useQueryClient();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const keys = useRef(new Map<string, string>());
  const run = useCallback<Action['run']>(
    async (name, call) => {
      setPending(true);
      setError(null);
      const key = keys.current.get(name) ?? newUlid();
      keys.current.set(name, key);
      try {
        const out = await call(key);
        keys.current.delete(name);
        return out;
      } catch (err) {
        if (err instanceof ApiError) {
          keys.current.delete(name);
          // Someone changed it under us, or the role changed: show what is true now.
          if (err.code === 'precondition_failed' || err.code === 'forbidden') void qc.invalidateQueries({ queryKey: ['biz'] });
        }
        setError(err);
        return null;
      } finally {
        setPending(false);
      }
    },
    [qc],
  );
  return { pending, error, run, clearError: () => setError(null) };
}
