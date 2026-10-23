import { useQuery } from '@tanstack/react-query';
import { useSessionState } from '@todayz/auth';
import { listBusinesses, rememberedStore, type ShopRole } from '../app/shop';

export interface CurrentStore {
  bid: string;
  sid: string;
  name: string;
  role: ShopRole;
}

export type CurrentStoreState =
  | { status: 'loading' }
  | { status: 'error'; retry: () => void }
  | { status: 'none' }
  | { status: 'ready'; store: CurrentStore };

/** Cashiers and above handle orders; cancelling an accepted order needs manager or above (D-65). */
export function canCancelAccepted(role: ShopRole): boolean {
  return role === 'owner' || role === 'manager';
}

/**
 * The shop this device works in, and the signed-in person's role there. The role comes from the same
 * `listBusinesses` answer the shop picker uses (W1-04, W1-08), so there is no second source. The
 * remembered store wins; with exactly one store it is that store. Several stores and none remembered
 * is "none": the page sends the person to the picker.
 */
export function useCurrentStore(): CurrentStoreState {
  const { status } = useSessionState();
  const q = useQuery({ queryKey: ['businesses'], queryFn: () => listBusinesses(), enabled: status === 'signedIn', retry: false });
  if (q.isError) return { status: 'error', retry: () => void q.refetch() };
  if (!q.data) return { status: 'loading' };
  const remembered = rememberedStore();
  const all = q.data.items.flatMap((b) => b.stores.map((s) => ({ b, s })));
  const pick = all.find((x) => x.s.id === remembered) ?? (all.length === 1 ? all[0] : undefined);
  if (!pick) return { status: 'none' };
  return { status: 'ready', store: { bid: pick.b.id, sid: pick.s.id, name: pick.s.name, role: pick.b.role } };
}
