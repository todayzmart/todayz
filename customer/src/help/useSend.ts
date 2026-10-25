import { ApiError } from '@todayz/api-client';
import { useCallback, useRef, useState } from 'react';
import { newUlid } from '../orders/ulid';

export interface Send {
  pending: boolean;
  error: unknown;
  /** Runs `call` with this attempt's Idempotency-Key; undefined when it failed (see `error`). */
  run<T>(call: (key: string) => Promise<T>): Promise<T | undefined>;
}

/**
 * One Idempotency-Key per message: a retry after a lost answer reuses it, so the ticket or reply is
 * made once. The key is dropped once the server has answered (success or a refusal).
 */
export function useSend(): Send {
  const key = useRef<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const run = useCallback(async <T,>(call: (key: string) => Promise<T>) => {
    key.current ??= newUlid();
    setPending(true);
    setError(null);
    try {
      const out = await call(key.current);
      key.current = null;
      return out;
    } catch (err) {
      if (err instanceof ApiError && err.status < 500) key.current = null;
      setError(err);
      return undefined;
    } finally {
      setPending(false);
    }
  }, []);
  return { pending, error, run };
}
