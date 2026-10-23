// The product page (C-08) is built from what the shop page or a search result already returned. Those
// screens seed this cache; a direct visit falls back to paging the shop's items until the item is found.
import { listDiscoverStoreItems } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import type { QueryClient } from '@tanstack/react-query';

export interface CachedItem {
  item: DiscoverSearchItem;
  serverNow: string;
}

export const itemKey = (slug: string, storeItemId: string) => ['discover', 'item', slug, storeItemId] as const;

export function seedItems(qc: QueryClient, slug: string, items: readonly DiscoverSearchItem[], serverNow: string): void {
  for (const item of items) {
    qc.setQueryData<CachedItem>(itemKey(slug, item.storeItemId), { item, serverNow });
  }
}

const MAX_PAGES = 10;

/** Pages through the shop's catalogue (100 per page) looking for one item; null when it is not listed. */
export async function findItem(slug: string, storeItemId: string): Promise<CachedItem | null> {
  let cursor: string | undefined;
  for (let page = 0; page < MAX_PAGES; page++) {
    const r = await listDiscoverStoreItems(slug, { limit: 100, ...(cursor ? { cursor } : {}) });
    const item = r.items.find((i) => i.storeItemId === storeItemId);
    if (item) return { item, serverNow: r.serverNow };
    if (!r.nextCursor) return null;
    cursor = r.nextCursor;
  }
  return null;
}
