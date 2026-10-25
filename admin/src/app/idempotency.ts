import { newUlid } from '@todayz/auth';
import { ApiError } from '@todayz/api-client';
import { useCallback, useRef } from 'react';

type Call<T> = (headers: Record<string, string>) => Promise<T>;

/**
 * One Idempotency-Key per pending change: a retry after a network failure resends the same key (the
 * server answers the first result again), and any server answer, success or problem, ends it.
 */
export function useIdempotent() {
  const key = useRef<string | null>(null);
  return useCallback(async <T,>(call: Call<T>): Promise<T> => {
    key.current ??= newUlid();
    try {
      const out = await call({ 'Idempotency-Key': key.current });
      key.current = null;
      return out;
    } catch (err) {
      if (err instanceof ApiError) key.current = null;
      throw err;
    }
  }, []);
}
