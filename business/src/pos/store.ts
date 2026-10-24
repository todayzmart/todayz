// The shop and role the counter works in. Online it is the same answer the Orders page uses; the
// last good answer is kept on the device so the counter still opens with no network (the server
// checks the role again when sales sync, so a stale role cannot do more than the server allows).
// TODO(lead): confirm that caching the store and role for offline POS is acceptable (permissions).
import { useEffect } from 'react';
import { useCurrentStore, type CurrentStore, type CurrentStoreState } from '../orders/store';

const KEY = 'todayz.biz.pos.store';

function readCached(): CurrentStore | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<CurrentStore>;
    return typeof v.bid === 'string' && typeof v.sid === 'string' && typeof v.name === 'string' && typeof v.role === 'string' ? (v as CurrentStore) : null;
  } catch {
    return null;
  }
}

function writeCached(s: CurrentStore) {
  try {
    localStorage.setItem(KEY, JSON.stringify(s));
  } catch {
    // Private mode: the counter simply needs the network the next time it opens.
  }
}

export function clearCachedPosStore() {
  try {
    localStorage.removeItem(KEY);
  } catch {
    // Nothing to clear.
  }
}

export function usePosStore(): CurrentStoreState {
  const current = useCurrentStore();
  const ready = current.status === 'ready' ? current.store : null;
  useEffect(() => {
    if (ready) writeCached(ready);
  }, [ready]);
  if (current.status === 'error') {
    const cached = readCached();
    if (cached) return { status: 'ready', store: cached };
  }
  return current;
}
