import { ApiError } from '@todayz/api-client';
import { newUlid } from '@todayz/auth';
import { useCallback, useRef, useState } from 'react';

export interface IdempotentAction {
  pending: boolean;
  error: unknown;
  /**
   * Runs `call` with an `Idempotency-Key`. The same `signature` (what is being asked, e.g. the
   * request body as JSON) reuses its key only after a network failure, so a retry of a lost request
   * cannot act twice; any answer from the server, good or bad, retires the key. Resolves to null on
   * a failure (the error is in `error`).
   */
  run<T>(signature: string, call: (key: string) => Promise<T>): Promise<T | null>;
  clearError(): void;
}

export function useIdempotentAction(): IdempotentAction {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const keys = useRef(new Map<string, string>());
  const run = useCallback(async <T,>(signature: string, call: (key: string) => Promise<T>): Promise<T | null> => {
    setPending(true);
    setError(null);
    const key = keys.current.get(signature) ?? newUlid();
    keys.current.set(signature, key);
    try {
      const out = await call(key);
      keys.current.delete(signature);
      return out;
    } catch (err) {
      if (err instanceof ApiError) keys.current.delete(signature);
      setError(err);
      return null;
    } finally {
      setPending(false);
    }
  }, []);
  return { pending, error, run, clearError: () => setError(null) };
}

/** The i18n key suffix (`stock.error.<suffix>`) for a failed stock call; unknown codes are `generic`. */
export function stockErrorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'validation_failed':
      case 'forbidden':
      case 'conflict':
      case 'not_found':
      case 'precondition_failed':
      case 'precondition_required':
        return err.code;
    }
  }
  return 'generic';
}
