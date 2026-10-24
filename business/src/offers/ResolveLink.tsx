import { useEffect, useRef } from 'react';
import type { CurrentStore } from '../orders/store';
import { useItemsById } from '../stock/queries';
import type { OfferDraft } from './form';
import { emptyDraft } from './form';
import { toPicked } from './picked';
import type { OffersSearch } from './deeplink';
import { useExpiringBatches } from './queries';

/**
 * Insight deep links (D-99) open the form prefilled and create nothing. Ids this store does not know
 * are dropped; when none of a link's ids is known, no form opens.
 */
export function ResolveLink({ store, search, onOpen }: { store: CurrentStore; search: OffersSearch; onOpen: (draft: Partial<OfferDraft>) => void }) {
  const ids = search.offer === 'item' && search.item ? [search.item] : search.offer === 'pair' ? (search.items ?? []) : [];
  const items = useItemsById(store, ids);
  const batches = useExpiringBatches(store, search.offer === 'flash');
  const opened = useRef(false);

  const itemsDone = items.every((q) => !q.isPending);
  const found = items.flatMap((q) => (q.data ? [toPicked(q.data)] : []));
  const batchesDone = search.offer !== 'flash' || !batches.isPending;

  useEffect(() => {
    if (opened.current || !search.offer || !itemsDone || !batchesDone) return;
    opened.current = true;
    if (search.offer === 'item' && found.length === 1) {
      onOpen(emptyDraft({ choice: 'percent', scope: 'items', items: found, ...(search.pct !== undefined ? { value: String(search.pct) } : {}) }));
    } else if (search.offer === 'pair' && found.length > 0) {
      // TODO(lead): a two-item deal has no offer kind of its own; it opens as a percent offer on both items.
      onOpen(emptyDraft({ choice: 'percent', scope: 'items', items: found }));
    } else if (search.offer === 'flash') {
      const known = new Set((batches.data ?? []).map((b) => b.id));
      const batchIds = (search.batches ?? []).filter((id) => known.has(id));
      if (batchIds.length > 0) onOpen(emptyDraft({ choice: 'flash', batchIds }));
    }
  });
  return null;
}
