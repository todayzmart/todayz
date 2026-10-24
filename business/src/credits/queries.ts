import { getStore } from '@todayz/api-client/gen/business/business';
import { getBusinessReferrals } from '@todayz/api-client/gen/referrals/referrals';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { getCreditAccount, type CreditAccount } from './api';

// Keys carry only the business and store ids; the referral code is data, never part of a key.
export const creditsKey = (bid: string) => ['credits', bid] as const;
export const referralsKey = (bid: string) => ['credits', bid, 'referrals'] as const;
export const shortCodeKey = (bid: string, sid: string) => ['credits', bid, 'store', sid] as const;

/** The balance and the lines, a cursor page at a time; `fetchNextPage` loads older lines. */
export function useCreditAccount(bid: string) {
  return useInfiniteQuery({
    queryKey: creditsKey(bid),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => getCreditAccount(bid, pageParam),
    getNextPageParam: (last: CreditAccount) => last.nextCursor ?? undefined,
    refetchOnMount: 'always',
    retry: false,
  });
}

export function useReferrals(bid: string) {
  return useQuery({ queryKey: referralsKey(bid), queryFn: () => getBusinessReferrals(bid), refetchOnMount: 'always', retry: false });
}

/** The store short code of the standee link (not secret: it is printed on the standee). */
export function useShortCode(bid: string, sid: string) {
  return useQuery({ queryKey: shortCodeKey(bid, sid), queryFn: async () => (await getStore(bid, sid)).shortCode });
}
