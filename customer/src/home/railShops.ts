import type { DiscoverStoreCard } from '@todayz/api-client/gen/model/discoverStoreCard';
import { useMemo } from 'react';

/** How many of the nearest shops feed the Home rails. */
const SHOPS = 3;

/** The nearest shops that are open and take orders, in the list's order (nearest first). */
export function useRailShops(stores: readonly DiscoverStoreCard[]): DiscoverStoreCard[] {
  return useMemo(() => stores.filter((s) => s.open && s.bookable).slice(0, SHOPS), [stores]);
}
