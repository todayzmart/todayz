import { ApiError } from '@todayz/api-client';
import {
  confirmVoiceStockDraft,
  createVoiceStockDraft,
  getVoiceStockAvailability,
  getVoiceStockDraft,
} from '@todayz/api-client/gen/voice/voice';
import type { VoiceStockConfirmResult } from '@todayz/api-client/gen/model/voiceStockConfirmResult';
import type { VoiceStockDecision } from '@todayz/api-client/gen/model/voiceStockDecision';
import type { VoiceStockDraft } from '@todayz/api-client/gen/model/voiceStockDraft';
import type { VoiceStockLanguage } from '@todayz/api-client/gen/model/voiceStockLanguage';
import { useQuery, type QueryClient } from '@tanstack/react-query';
import type { CurrentStore } from '../orders/store';
import { stockKey } from '../stock/queries';

/** How often a processing draft is read again. */
export const POLL_MS = 1000;

export const voiceKey = (sid: string) => ['voice', sid] as const;
export const availabilityKey = (sid: string) => [...voiceKey(sid), 'availability'] as const;
export const draftKey = (sid: string, id: string) => [...voiceKey(sid), 'draft', id] as const;

/**
 * Whether voice update is on for the shop. The API answers the common 404 while it is off (a
 * switch, the feature flag or a provider key), so `data` is null then and the screen hides.
 */
export function useVoiceAvailability(store: CurrentStore | null) {
  return useQuery({
    queryKey: availabilityKey(store?.sid ?? ''),
    enabled: store !== null,
    queryFn: async () => {
      if (!store) return null;
      try {
        return await getVoiceStockAvailability(store.bid, store.sid);
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    staleTime: 60_000,
    retry: false,
  });
}

/** The draft; read again every POLL_MS while the speech and the model are still working. */
export function useVoiceDraft(store: CurrentStore, id: string | null) {
  return useQuery({
    queryKey: draftKey(store.sid, id ?? ''),
    enabled: id !== null,
    queryFn: () => getVoiceStockDraft(store.bid, store.sid, id ?? ''),
    refetchInterval: (q) => (q.state.data?.status === 'processing' ? POLL_MS : false),
    retry: false,
  });
}

/** Sends one recording; the answer is the draft id at once (202), the lines come later. */
export async function sendRecording(s: CurrentStore, audio: Blob, language: VoiceStockLanguage | null): Promise<string> {
  const out = await createVoiceStockDraft(s.bid, s.sid, language ? { audio, language } : { audio });
  return out.draftId;
}

/** Confirms (or, with every line discarded, discards) the draft; a repeat with the same key answers the first result. */
export async function confirmDraft(
  qc: QueryClient,
  s: CurrentStore,
  draft: VoiceStockDraft,
  lines: VoiceStockDecision[],
  key: string,
): Promise<VoiceStockConfirmResult> {
  const out = await confirmVoiceStockDraft(s.bid, s.sid, draft.draftId, { lines }, { idempotencyKey: key });
  // Stock, prices and sales changed: the other screens read again.
  void qc.invalidateQueries({ queryKey: stockKey(s.sid) });
  void qc.invalidateQueries({ queryKey: draftKey(s.sid, draft.draftId) });
  return out;
}

/** The i18n key suffix (`voice.error.<suffix>`) for a failed call; unknown codes are `generic`. */
export function voiceErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 413) return 'too_large';
    switch (err.code) {
      case 'ai_daily_cap_reached':
      case 'validation_failed':
      case 'invalid_transition':
      case 'not_found':
      case 'forbidden':
        return err.code;
    }
  }
  return 'generic';
}

/** The i18n key suffix (`voice.failed.<suffix>`) for a failed draft. */
export function failureKey(reason: string | null): string {
  switch (reason) {
    case 'empty':
      return 'empty';
    case 'timeout':
    case 'rate_limited':
      return 'busy';
    default:
      return 'generic';
  }
}
