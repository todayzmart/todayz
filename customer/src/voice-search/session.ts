// Voice search state that outlives the mic: the block for this page session, and the
// voice_search_used outcome C-05 settles when the customer searches or leaves.
import type { AiLanguage } from '@todayz/api-client/gen/model/aiLanguage';
import { useEffect, useRef } from 'react';
import { clampInt, track } from '../ai/events';

export const MAX_EVENT_SECONDS = 15;

/** What one voice search heard: the words for the box, and what the event needs. */
export interface Heard {
  transcript: string;
  lang: AiLanguage;
  seconds: number;
}

/** This page session: 'failed' hides the mic, 'capped' disables it. A reload starts again. */
let blocked: 'failed' | 'capped' | null = null;

export const getVoiceBlock = () => blocked;
export function setVoiceBlock(b: 'failed' | 'capped'): void {
  blocked = b;
}

/** Tests only. */
export function resetVoiceSearchForTests(): void {
  blocked = null;
}

/**
 * The voice_search_used outcome for C-05. `heard` when a transcript fills the box; `searched` when
 * the customer searches (answers the `source` for that search); a new recording or leaving the page
 * with a transcript still waiting is `abandoned`.
 */
export function useVoiceOutcome() {
  const pending = useRef<Heard | null>(null);
  const settle = (outcome: 'searched' | 'edited_then_searched' | 'abandoned') => {
    const h = pending.current;
    if (!h) return;
    pending.current = null;
    track('voice_search_used', { lang: h.lang, seconds: clampInt(h.seconds, MAX_EVENT_SECONDS), outcome });
  };
  const settleRef = useRef(settle);
  useEffect(() => {
    settleRef.current = settle;
  });
  useEffect(() => () => settleRef.current('abandoned'), []);
  return {
    heard(h: Heard) {
      settle('abandoned');
      pending.current = h;
    },
    searched(text: string): 'voice' | undefined {
      const h = pending.current;
      if (!h) return undefined;
      settle(text.trim() === h.transcript ? 'searched' : 'edited_then_searched');
      return 'voice';
    },
  };
}
