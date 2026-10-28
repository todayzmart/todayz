import { ApiError } from '@todayz/api-client';
import { createStandee, listStandees, markStoreSheetPrinted, requestStandeeReorder } from '@todayz/api-client/gen/standee/standee';
import type { Standee } from '@todayz/api-client/gen/model/standee';
import { useQuery, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../../orders/store';
import { overviewKey } from '../../overview/queries';

// Keys carry only the store id; a standee's link is data, never part of a key.
export const standeesKey = (sid: string) => ['standees', sid] as const;

/** The store's standees, newest first, with their scans → sign-ups → qualified counts (W3-09, D-223). */
export function useStandees(store: CurrentStore) {
  return useQuery({ queryKey: standeesKey(store.sid), queryFn: () => listStandees(store.bid, store.sid), refetchOnMount: 'always', retry: false });
}

/** Adds a standee for a campaign; `key` is the attempt's Idempotency-Key. */
export async function addStandee(qc: QueryClient, s: CurrentStore, campaign: string, key: string): Promise<Standee> {
  try {
    return await createStandee(s.bid, s.sid, { campaign }, { idempotencyKey: key });
  } finally {
    void qc.invalidateQueries({ queryKey: standeesKey(s.sid) });
  }
}

/** Asks for a new printed standee. A request already waiting is answered unchanged by the server. */
export async function reorderStandee(qc: QueryClient, s: CurrentStore, id: string, key: string): Promise<Standee> {
  try {
    return await requestStandeeReorder(s.bid, s.sid, id, { idempotencyKey: key });
  } finally {
    void qc.invalidateQueries({ queryKey: standeesKey(s.sid) });
  }
}

/**
 * Records that the store sheet was printed, which ticks the setup step "Print the QR standee" (D-281). Fire and forget:
 * printing never waits on it, and a failure only leaves the step for the next Print.
 */
export function noteSheetPrinted(qc: QueryClient, s: CurrentStore): void {
  markStoreSheetPrinted(s.bid, s.sid).then(
    () => void qc.invalidateQueries({ queryKey: overviewKey(s.sid) }),
    () => undefined,
  );
}

/** The i18n suffix for an error (`standee.campaigns.error.<key>`). */
export function standeeErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'validation_failed':
      case 'forbidden':
      case 'not_found':
      case 'standee_limit_reached':
        return err.code;
    }
  }
  return 'generic';
}
