import { ApiError } from '@todayz/api-client';
import {
  acceptOrder,
  cancelStoreOrder,
  confirmOrderReturn,
  dispatchOrder,
  failOrderDelivery,
  getStoreOrder,
  listStoreOrders,
  readyOrder,
  rejectOrder,
  verifyOrderCode,
} from '@todayz/api-client/gen/orders/orders';
import type { AcceptLine } from '@todayz/api-client/gen/model/acceptLine';
import type { ConfirmReturnRequestOutcome } from '@todayz/api-client/gen/model/confirmReturnRequestOutcome';
import type { DeliveryFailedReason } from '@todayz/api-client/gen/model/deliveryFailedReason';
import type { PaymentMethod } from '@todayz/api-client/gen/model/paymentMethod';
import type { RejectReason } from '@todayz/api-client/gen/model/rejectReason';
import type { ShopCancelReason } from '@todayz/api-client/gen/model/shopCancelReason';
import type { StoreOrder } from '@todayz/api-client/gen/model/storeOrder';
import type { StoreOrderList } from '@todayz/api-client/gen/model/storeOrderList';
import { newUlid, useSession } from '@todayz/auth';
import { useRealtimeInvalidation, type RealtimeStatus } from '@todayz/realtime';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';
import { API_URL } from '../app/env';
import { asksKey } from '../asks/api';
import type { CurrentStore } from './store';

/**
 * B-04 tabs backed by the API's `status` filter. Out for delivery and Returns to confirm (D-69) always
 * show: the shop list does not say whether a store delivers, and an empty tab says what lands there.
 */
export const TABS = ['new', 'accepted', 'ready', 'out_for_delivery', 'returns', 'done', 'missed'] as const;
export type OrderTab = (typeof TABS)[number];

export const ordersKey = (sid: string) => ['orders', sid] as const;
export const listKey = (sid: string, tab: OrderTab) => ['orders', sid, 'list', tab] as const;
export const orderKey = (sid: string, id: string) => ['orders', sid, 'order', id] as const;

export function useOrderList(store: CurrentStore, tab: OrderTab) {
  return useQuery({
    queryKey: listKey(store.sid, tab),
    queryFn: () => listStoreOrders(store.bid, store.sid, { status: tab, limit: 50 }),
    refetchOnMount: 'always',
  });
}

export function useOrder(store: CurrentStore, id: string) {
  return useQuery({
    queryKey: orderKey(store.sid, id),
    queryFn: () => getStoreOrder(store.bid, store.sid, id),
    retry: (n, err) => !(err instanceof ApiError && err.status === 404) && n < 1,
  });
}

/**
 * Live updates over the store stream (`store.stream`, W2-05). Each `order.created` or
 * `order.updated` refetches the order lists and that order; a `reset` or a polling tick refetches
 * everything under `['orders', sid]`. Returns the stream status, so the page can say when it is
 * polling instead.
 */
export function useOrdersStream(store: CurrentStore): RealtimeStatus {
  const session = useSession();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<RealtimeStatus>('connecting');
  useRealtimeInvalidation(
    {
      url: `${API_URL}/v1/biz/businesses/${store.bid}/stores/${store.sid}/stream`,
      ownerKey: store.sid,
      getToken: () => session.getToken(),
      refresh: () => session.refresh(),
      queryClient,
      resetKeys: [ordersKey(store.sid), asksKey(store.sid)],
      onStatus: setStatus,
    },
    {
      'order.created': () => [ordersKey(store.sid)],
      'order.updated': () => [ordersKey(store.sid)],
      // B-20 (W3-16): a new ask, or one the customer closed.
      'ask.created': () => [asksKey(store.sid)],
      'ask.closed': () => [asksKey(store.sid)],
    },
  );
  return status;
}

/** Puts an action's answer straight into the cache, then refetches the lists. */
export function applyOrder(qc: QueryClient, sid: string, order: StoreOrder): void {
  qc.setQueryData(orderKey(sid, order.id), order);
  void qc.invalidateQueries({ queryKey: [...ordersKey(sid), 'list'] });
}

export type ActionKind = 'accept' | 'acceptPart' | 'reject' | 'ready' | 'cancel' | 'verify' | 'dispatch' | 'failDelivery' | 'confirmReturn';

/** The i18n key suffix for an action failure; unknown codes fall back to `generic`. */
export function errorKey(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'insufficient_stock':
      case 'invalid_transition':
      case 'forbidden':
      case 'validation_failed':
      case 'expired':
        return err.code;
    }
  }
  return 'generic';
}

export interface ActionRunner {
  pending: ActionKind | null;
  error: unknown;
  run<T extends StoreOrder>(kind: ActionKind, call: (headers: Record<string, string>) => Promise<T>): Promise<T | null>;
  clearError(): void;
}

/**
 * Runs one order action. The `Idempotency-Key` is reused when the same action is retried after a
 * network failure and replaced after any answer from the server (success or a problem), so a retry
 * of a lost request cannot act twice.
 */
export function useOrderAction(store: CurrentStore): ActionRunner {
  const qc = useQueryClient();
  const [pending, setPending] = useState<ActionKind | null>(null);
  const [error, setError] = useState<unknown>(null);
  const keys = useRef(new Map<ActionKind, string>());
  const run = useCallback<ActionRunner['run']>(
    async (kind, call) => {
      setPending(kind);
      setError(null);
      const key = keys.current.get(kind) ?? newUlid();
      keys.current.set(kind, key);
      try {
        const order = await call({ 'Idempotency-Key': key });
        keys.current.delete(kind);
        applyOrder(qc, store.sid, order);
        return order;
      } catch (err) {
        if (err instanceof ApiError) {
          keys.current.delete(kind);
          // The order moved under us: show what it is now.
          if (err.code === 'invalid_transition' || err.code === 'insufficient_stock') void qc.invalidateQueries({ queryKey: ordersKey(store.sid) });
        }
        setError(err);
        return null;
      } finally {
        setPending(null);
      }
    },
    [qc, store.sid],
  );
  return { pending, error, run, clearError: () => setError(null) };
}

export const acceptCall = (s: CurrentStore, id: string) => (headers: Record<string, string>) =>
  acceptOrder(s.bid, s.sid, id, {}, { headers });
/** Accept part (D-28): every line named with the quantity the shop gives. */
export const acceptPartCall = (s: CurrentStore, id: string, lines: AcceptLine[]) => (headers: Record<string, string>) =>
  acceptOrder(s.bid, s.sid, id, { lines }, { headers });
export const rejectCall = (s: CurrentStore, id: string, reason: RejectReason, note?: string) => (headers: Record<string, string>) =>
  rejectOrder(s.bid, s.sid, id, note ? { reason, note } : { reason }, { headers });
export const readyCall = (s: CurrentStore, id: string) => (headers: Record<string, string>) => readyOrder(s.bid, s.sid, id, { headers });
export const cancelCall = (s: CurrentStore, id: string, reason: ShopCancelReason, note?: string) => (headers: Record<string, string>) =>
  cancelStoreOrder(s.bid, s.sid, id, note ? { reason, note } : { reason }, { headers });
/** Delivery (BK-24): accepted to out_for_delivery. */
export const dispatchCall = (s: CurrentStore, id: string) => (headers: Record<string, string>) => dispatchOrder(s.bid, s.sid, id, { headers });
/** Delivery failed (D-60): `other` needs a note; the reservation stays until the return is confirmed (D-69). */
export const failDeliveryCall = (s: CurrentStore, id: string, reason: DeliveryFailedReason, note?: string) => (headers: Record<string, string>) =>
  failOrderDelivery(s.bid, s.sid, id, note ? { reason, note } : { reason }, { headers });
/** Did the goods come back (D-69): `returned` releases the stock, `damaged` (manager and above) writes it off. */
export const confirmReturnCall = (s: CurrentStore, id: string, outcome: ConfirmReturnRequestOutcome) => (headers: Record<string, string>) =>
  confirmOrderReturn(s.bid, s.sid, id, { outcome }, { headers });
export const verifyCall = (s: CurrentStore, id: string, code: string, paymentMethod: PaymentMethod) => (headers: Record<string, string>) =>
  verifyOrderCode(s.bid, s.sid, id, { code, paymentMethod }, { headers });

/** The count of new orders, for the tab badge. */
export function newCount(list: StoreOrderList | undefined): number {
  return list?.items.length ?? 0;
}

/** Milliseconds from the server's clock to this device's, taken when the answer arrived. */
export function useServerOffset(serverNow: string | undefined, updatedAt: number): number {
  return serverNow ? Date.parse(serverNow) - updatedAt : 0;
}

/** Seconds left until `deadline` by the server's clock, ticking once a second. Null when there is no deadline. */
/** The device time, refreshed every minute: for "failed 2 h 5 min ago" labels. */
export function useNow(everyMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), everyMs);
    return () => clearInterval(t);
  }, [everyMs]);
  return now;
}

export function useSecondsLeft(deadline: string | null | undefined, offsetMs: number): number | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!deadline) return undefined;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [deadline]);
  if (!deadline) return null;
  return Math.max(0, Math.ceil((Date.parse(deadline) - (now + offsetMs)) / 1000));
}
