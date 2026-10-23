import { useRealtimeInvalidation } from '@todayz/realtime';
import { useQueryClient } from '@tanstack/react-query';
import { useSession, useSessionState } from '@todayz/auth';
import { API_URL } from '../app/env';

let streamFetch: typeof fetch | undefined;
/** Tests only: the fetch the live stream uses. */
export function setStreamFetchForTests(f: typeof fetch | undefined): void {
  streamFetch = f;
}

/**
 * The customer's live stream (`/v1/me/stream`, W2-05): `order.updated` refetches that order and the
 * list. Events carry no data, only what changed, so the refetch is the truth. After three failed
 * connects the client polls every 15 s (the package's fallback), so tracking still moves.
 */
export function useMyOrderStream(): void {
  const session = useSession();
  const { status } = useSessionState();
  const queryClient = useQueryClient();
  useRealtimeInvalidation(
    {
      url: `${API_URL}/v1/me/stream`,
      ownerKey: status === 'signedIn' ? 'me' : null,
      enabled: status === 'signedIn',
      getToken: () => session.getToken(),
      refresh: () => session.refresh(),
      queryClient,
      resetKeys: [['orders'], ['order']],
      ...(streamFetch ? { fetch: streamFetch } : {}),
    },
    {
      'order.updated': (e) => [['order', e.id], ['orders']],
    },
  );
}
