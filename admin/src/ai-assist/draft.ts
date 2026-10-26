import { ApiError } from '@todayz/api-client';
import type { AiConfirmResult } from '@todayz/api-client/gen/model/aiConfirmResult';
import type { AiConfirmOutcome } from '@todayz/api-client/gen/model/aiConfirmOutcome';
import type { AiDraftCreated } from '@todayz/api-client/gen/model/aiDraftCreated';
import type { AiDraftStatus } from '@todayz/api-client/gen/model/aiDraftStatus';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useIdempotent } from '../app/idempotency';

/**
 * How often a draft still `processing` is read again. A client cadence, not a product rule: the
 * server closes a draft that outlives the provider timeout as failed, so polling always ends.
 */
export const POLL_MS = 1500;

/** What the person chose for one line. Nothing chosen is sent as `discarded` (04 §2.8). */
export type Choice = AiConfirmOutcome;

/** What a failed assistant call means for the person, as an `aiAssist.error.<key>` suffix. */
export type AssistErrorKey = 'cap' | 'off' | 'moved' | 'invalid' | 'forbidden' | 'network' | 'generic';

export function assistErrorKey(err: unknown): AssistErrorKey {
  if (!(err instanceof ApiError)) return 'network';
  if (err.code === 'ai_daily_cap_reached') return 'cap';
  // A feature switched off, or a draft that is someone else's, answers the common 404 (11 §1).
  if (err.code === 'not_found') return 'off';
  if (err.code === 'invalid_transition' || err.code === 'conflict') return 'moved';
  if (err.code === 'validation_failed') return 'invalid';
  if (err.code === 'forbidden') return 'forbidden';
  return 'generic';
}

/** The field reasons of a confirm's 422 per line index: `body.lines[1].value.productId: same` → `{1: 'same'}`. */
export function lineReasons(err: unknown): Record<number, string> {
  const out: Record<number, string> = {};
  if (!(err instanceof ApiError)) return out;
  for (const [path, reason] of Object.entries(err.fields)) {
    const m = /^body\.lines\[(\d+)\]/.exec(path);
    if (m) out[Number(m[1])] ??= reason;
  }
  return out;
}

interface Draftish {
  draftId: string;
  status: AiDraftStatus;
}

interface Calls<D extends Draftish, B> {
  /** The query key prefix of the screen's draft, e.g. `['admin', 'ai', 'triage', ticketId]`. */
  key: readonly unknown[];
  create: (headers: Record<string, string>) => Promise<AiDraftCreated>;
  get: (draftId: string) => Promise<D>;
  confirm: (draftId: string, body: B, headers: Record<string, string>) => Promise<AiConfirmResult>;
  /** Called after a confirm the server answered, to refresh what the screen shows. */
  settled?: (r: AiConfirmResult) => void;
}

/**
 * One admin assistant draft (04 §2.8): create answers 202 with an id, the draft is read again while
 * the model works, and confirm applies only what the person accepted or edited. The draft lives in
 * component state, so leaving the screen forgets it; nothing is applied without a confirm.
 */
export function useAssistDraft<D extends Draftish, B>(c: Calls<D, B>) {
  const [draftId, setDraftId] = useState<string | null>(null);
  const createOnce = useIdempotent();
  const confirmOnce = useIdempotent();
  const create = useMutation({
    mutationFn: () => createOnce((h) => c.create(h)),
    onSuccess: (r) => setDraftId(r.draftId),
  });
  const draft = useQuery({
    queryKey: [...c.key, draftId],
    queryFn: () => c.get(draftId ?? ''),
    enabled: draftId !== null,
    refetchInterval: (q) => (q.state.data?.status === 'processing' ? POLL_MS : false),
    refetchOnWindowFocus: false,
  });
  const confirm = useMutation({
    mutationFn: (body: B) => confirmOnce((h) => c.confirm(draftId ?? '', body, h)),
    onSuccess: (r) => {
      void draft.refetch();
      c.settled?.(r);
    },
  });
  const d = draftId === null ? undefined : draft.data;
  return {
    start: () => {
      confirm.reset();
      create.mutate();
    },
    /** Throws the draft away on this screen only, so the person can ask again. */
    reset: () => {
      setDraftId(null);
      create.reset();
      confirm.reset();
    },
    draft: d,
    working: create.isPending || (draftId !== null && (draft.isPending || d?.status === 'processing')),
    error: create.error ?? (draft.isError ? draft.error : null),
    confirm,
  };
}

/** One line's decision: an edit carries the person's value; a line nothing was chosen for is `discarded`. */
export function decision<V>(lineId: string, choice: Choice, value: V): { lineId: string; outcome: Choice; value?: V } {
  return choice === 'edited' ? { lineId, outcome: choice, value } : { lineId, outcome: choice };
}
