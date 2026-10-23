import type { StoreOrderSummary } from '@todayz/api-client/gen/model/storeOrderSummary';
import { useCallback, useEffect, useState } from 'react';

/**
 * Which new orders still need the shop's attention (the sound loops until this is empty).
 *
 * Listing marks the listed new orders as seen on the server, so `seen` is only trusted on the first
 * load of a session: an order already seen then was handled before this page opened. Every order
 * that shows up later is alerting until someone opens, accepts or rejects it. The state lives at
 * module level so going to an order and back does not start the sound again for the same order.
 */
interface StoreAlerts {
  initialised: boolean;
  acknowledged: Set<string>;
}

const stores = new Map<string, StoreAlerts>();
const listeners = new Set<() => void>();

function forStore(sid: string): StoreAlerts {
  let s = stores.get(sid);
  if (!s) {
    s = { initialised: false, acknowledged: new Set() };
    stores.set(sid, s);
  }
  return s;
}

function notify(): void {
  listeners.forEach((l) => l());
}

/** The shop opened, accepted or rejected this order: stop alerting on it. */
export function acknowledgeOrder(sid: string, id: string): void {
  const s = forStore(sid);
  if (s.acknowledged.has(id)) return;
  s.acknowledged.add(id);
  notify();
}

export function resetAlerts(): void {
  stores.clear();
  notify();
}

/** The new orders that are still alerting, given the latest New list. */
export function useAlertingOrders(sid: string, items: readonly StoreOrderSummary[] | undefined): StoreOrderSummary[] {
  const [, bump] = useState(0);
  useEffect(() => {
    const l = () => bump((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  useEffect(() => {
    if (!items) return;
    const s = forStore(sid);
    if (!s.initialised) {
      s.initialised = true;
      for (const o of items) if (o.seen) s.acknowledged.add(o.id);
      notify();
    }
  }, [sid, items]);
  const s = forStore(sid);
  // Until the first load has been read, nothing alerts (avoids a chime for an order seen yesterday).
  if (!items || !s.initialised) return [];
  return items.filter((o) => !s.acknowledged.has(o.id));
}

/** For the detail page: opening an order acknowledges it. */
export function useAcknowledgeOnOpen(sid: string, id: string): void {
  const ack = useCallback(() => acknowledgeOrder(sid, id), [sid, id]);
  useEffect(ack, [ack]);
}
