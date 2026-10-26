// Which customer AI helpers this person has (GET /v1/me/ai/features, W3-12). Signed in only: a
// signed-out customer gets none of them, so the helpers render nothing and nothing is asked for.
import { ApiError } from '@todayz/api-client';
import { getMyAiFeatures } from '@todayz/api-client/gen/ai/ai';
import type { AiCustomerFeatures } from '@todayz/api-client/gen/model/aiCustomerFeatures';
import { useSessionState } from '@todayz/auth';
import { useQuery } from '@tanstack/react-query';

export const aiFeaturesKey = ['me', 'ai', 'features'] as const;

/** The features while signed in; undefined while signed out, loading, or when the call failed. */
export function useAiFeatures(): AiCustomerFeatures | undefined {
  const { status } = useSessionState();
  const signedIn = status === 'signedIn';
  const q = useQuery({
    queryKey: aiFeaturesKey,
    enabled: signedIn,
    staleTime: 5 * 60_000,
    retry: false,
    queryFn: () => getMyAiFeatures(),
  });
  return signedIn ? q.data : undefined;
}

/** The daily AI cap (`429 ai_daily_cap_reached`), shared by the three helpers. */
export function isCapReached(err: unknown): boolean {
  return err instanceof ApiError && err.code === 'ai_daily_cap_reached';
}
