import { useEffect, useRef } from 'react';
import {
  createRealtimeStream,
  type RealtimeEvent,
  type RealtimeStatus,
  type RealtimeTimers,
  type RealtimeVisibility,
} from './stream';

/** Structural subset of TanStack's QueryClient, so this package does not depend on it. */
export interface InvalidatingQueryClient {
  invalidateQueries(filters: { queryKey: readonly unknown[] }): unknown;
}

export type InvalidationMap = Record<string, (e: RealtimeEvent) => readonly (readonly unknown[])[]>;

export interface RealtimeInvalidationConfig {
  /** Stream URL (full, including the API origin). Changing it restarts the stream. */
  url: string;
  /** Changing the owner (for example the signed-in user id) restarts the stream. */
  ownerKey?: string | null;
  /** Set false to stay disconnected (signed out, no store picked). */
  enabled?: boolean;
  getToken(): string | null;
  refresh(): Promise<boolean>;
  queryClient: InvalidatingQueryClient;
  /** Every key this stream's events can touch; invalidated on `reset` and on each polling tick. */
  resetKeys: readonly (readonly unknown[])[];
  fetch?: typeof fetch;
  onStatus?(s: RealtimeStatus): void;
  timers?: RealtimeTimers;
}

/** Page visibility from `document`, safe where there is no document. */
export const pageVisibility: RealtimeVisibility = {
  isVisible: () => typeof document === 'undefined' || document.visibilityState !== 'hidden',
  subscribe(cb) {
    if (typeof document === 'undefined') return () => undefined;
    document.addEventListener('visibilitychange', cb);
    return () => document.removeEventListener('visibilitychange', cb);
  },
};

/**
 * Runs a realtime stream for the lifetime of the component and invalidates the query keys the map
 * returns for each event. Events carry no payload to apply: the refetch is the source of truth.
 */
export function useRealtimeInvalidation(config: RealtimeInvalidationConfig, map: InvalidationMap): void {
  const latest = useRef({ config, map });
  useEffect(() => {
    latest.current = { config, map };
  });
  const enabled = config.enabled ?? true;

  useEffect(() => {
    if (!enabled) return undefined;
    const invalidate = (keys: readonly (readonly unknown[])[]): void => {
      const { queryClient } = latest.current.config;
      for (const queryKey of keys) queryClient.invalidateQueries({ queryKey });
    };
    const stream = createRealtimeStream({
      url: config.url,
      getToken: () => latest.current.config.getToken(),
      refresh: () => latest.current.config.refresh(),
      ...(config.fetch ? { fetch: config.fetch } : {}),
      ...(config.timers ? { timers: config.timers } : {}),
      visibility: pageVisibility,
      onEvent: (e) => {
        const keysFor = latest.current.map[e.type];
        if (keysFor) invalidate(keysFor(e));
      },
      onReset: () => invalidate(latest.current.config.resetKeys),
      poll: () => invalidate(latest.current.config.resetKeys),
      onStatus: (s) => latest.current.config.onStatus?.(s),
    });
    return () => stream.stop();
    // fetch and timers are test seams and are read once per stream.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config.url, config.ownerKey, enabled]);
}
