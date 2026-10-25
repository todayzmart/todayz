// Asks (04 me/asks, D-101): query keys, the live stream and what a refused ask means for the sheet.
import { ApiError } from '@todayz/api-client';
import { closeMyAsk, getMyAsk, listMyAsks } from '@todayz/api-client/gen/asks/asks';
import type { AskDetail } from '@todayz/api-client/gen/model/askDetail';
import { useSession, useSessionState } from '@todayz/auth';
import { useRealtimeInvalidation } from '@todayz/realtime';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { API_URL } from '../app/env';

export const asksKey = ['me', 'asks'] as const;
export const askKey = (id: string) => ['me', 'asks', id] as const;

export const loadAsks = () => listMyAsks({ limit: 50 });
export const loadAsk = (id: string) => getMyAsk(id);
export const closeAsk = (id: string) => closeMyAsk(id);

/** After a create or close: the ask's own query gets the answer, the list is read again. */
export function storedAsk(qc: QueryClient, d: AskDetail): void {
  qc.setQueryData(askKey(d.ask.askId), d);
  void qc.invalidateQueries({ queryKey: asksKey, exact: true });
}

/** Why the sheet could not send. One copy for every reason a single shop is left out (01 §11.2). */
export type AskProblem =
  | { kind: 'limit'; limit: number | null }
  | { kind: 'open'; askId: string | null }
  | { kind: 'no_shops' }
  | { kind: 'store_unavailable' }
  | { kind: 'invalid' }
  | { kind: 'failed' };

export function askProblemOf(err: unknown): AskProblem {
  if (!(err instanceof ApiError)) return { kind: 'failed' };
  const ext = err.problem.extensions ?? {};
  switch (err.code) {
    case 'ask_limit_reached':
      return { kind: 'limit', limit: typeof ext['limit'] === 'number' ? ext['limit'] : null };
    case 'ask_already_open':
      return { kind: 'open', askId: typeof ext['askId'] === 'string' ? ext['askId'] : null };
    case 'ask_no_shops':
      return { kind: 'no_shops' };
    case 'ask_store_unavailable':
      return { kind: 'store_unavailable' };
    case 'validation_failed':
      return { kind: 'invalid' };
    default:
      return { kind: 'failed' };
  }
}

let streamFetch: typeof fetch | undefined;
/** Tests only: the fetch the live stream uses. */
export function setAskStreamFetchForTests(f: typeof fetch | undefined): void {
  streamFetch = f;
}

/**
 * C-26 live progress: `ask.updated` on `/v1/me/stream` refetches that ask and the list. Events carry
 * no data, so the refetch is the truth; after three failed connects the package polls (02 §5).
 */
export function useMyAskStream(): void {
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
      resetKeys: [asksKey],
      ...(streamFetch ? { fetch: streamFetch } : {}),
    },
    {
      'ask.updated': (e) => [askKey(e.id), asksKey],
    },
  );
}
