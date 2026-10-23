import { createContext, useContext, useSyncExternalStore } from 'react';

const KEY = 'todayz.admin.2fa';
/** What the OTP step said comes next; a reload in the middle of the step keeps routing the same way. */
const NEXT_KEY = 'todayz.admin.2fa.next';

export type NextStep = 'totp_enrol' | 'totp_verify';

export interface SecondFactorState {
  /** The authenticator step was passed in this tab. */
  done: boolean;
  /** What the OTP sign-in said comes next (`nextStep` of the verify answer); kept for this tab. */
  nextStep: NextStep | undefined;
}

export interface SecondFactor {
  getState(): SecondFactorState;
  subscribe(listener: () => void): () => void;
  setNextStep(step: NextStep | undefined): void;
  markDone(): void;
  clear(): void;
}

/**
 * Whether the person has passed the authenticator step, for routing only: it decides which screen
 * to show, never what the API allows (the server checks the token's `amr` on every admin call).
 * The flag lives in sessionStorage so a reload in the same tab does not ask again; it is a
 * timestamp, not a secret, and it goes with the tab. TODO(W1-01b): read it from the token's `amr`
 * claim or `/v1/me` once the contract says where.
 */
export function createSecondFactor(): SecondFactor {
  const read = (): boolean => {
    try {
      return sessionStorage.getItem(KEY) !== null;
    } catch {
      return false;
    }
  };
  const readNext = (): NextStep | undefined => {
    try {
      const v = sessionStorage.getItem(NEXT_KEY);
      return v === 'totp_enrol' || v === 'totp_verify' ? v : undefined;
    } catch {
      return undefined;
    }
  };
  const writeNext = (step: NextStep | undefined) => {
    try {
      if (step) sessionStorage.setItem(NEXT_KEY, step);
      else sessionStorage.removeItem(NEXT_KEY);
    } catch {
      // Private mode: a reload then asks for the code.
    }
  };
  let state: SecondFactorState = { done: read(), nextStep: readNext() };
  const listeners = new Set<() => void>();
  const set = (next: SecondFactorState) => {
    state = next;
    for (const l of listeners) l();
  };
  return {
    getState: () => state,
    subscribe(l) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    setNextStep(step) {
      writeNext(step);
      set({ ...state, nextStep: step });
    },
    markDone() {
      try {
        sessionStorage.setItem(KEY, new Date().toISOString());
      } catch {
        // Private mode: the next reload asks for the code again.
      }
      writeNext(undefined);
      set({ done: true, nextStep: undefined });
    },
    clear() {
      try {
        sessionStorage.removeItem(KEY);
      } catch {
        // Nothing stored, nothing to clear.
      }
      writeNext(undefined);
      set({ done: false, nextStep: undefined });
    },
  };
}

export const SecondFactorContext = createContext<SecondFactor | null>(null);

export function useSecondFactor(): SecondFactor {
  const v = useContext(SecondFactorContext);
  if (!v) throw new Error('useSecondFactor must be used inside <App>');
  return v;
}

export function useSecondFactorState(): SecondFactorState {
  const f = useSecondFactor();
  return useSyncExternalStore(f.subscribe, f.getState, f.getState);
}
