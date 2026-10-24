// The one unsaved basket of a signed-out customer (C-22, BSK-11): kept in this browser only, never sent
// anywhere until the customer signs in and chooses to save it. Nothing about the person is kept here.
import { useSyncExternalStore } from 'react';
import type { EditLine } from './model';

export interface DeviceBasket {
  name: string;
  lines: EditLine[];
}

const KEY = 'todayz.basket.device';
const EMPTY: DeviceBasket = { name: '', lines: [] };
const listeners = new Set<() => void>();
let cached: DeviceBasket | null = null;

function valid(l: unknown): l is EditLine {
  if (typeof l !== 'object' || l === null) return false;
  const v = l as Record<string, unknown>;
  return (
    typeof v['key'] === 'string' &&
    typeof v['label'] === 'string' &&
    Number.isInteger(v['quantity']) &&
    (typeof v['productId'] === 'string' || typeof v['text'] === 'string')
  );
}

function read(): DeviceBasket {
  if (cached) return cached;
  try {
    const raw = localStorage.getItem(KEY);
    const parsed = raw ? (JSON.parse(raw) as Partial<DeviceBasket>) : null;
    cached = parsed
      ? {
          name: typeof parsed.name === 'string' ? parsed.name : '',
          lines: Array.isArray(parsed.lines) ? parsed.lines.filter(valid).map((l) => ({ ...l, detail: l.detail ?? '' })) : [],
        }
      : EMPTY;
  } catch {
    cached = EMPTY;
  }
  return cached;
}

export function getDeviceBasket(): DeviceBasket {
  return read();
}

export function setDeviceBasket(next: DeviceBasket): void {
  cached = next.lines.length === 0 && !next.name ? EMPTY : next;
  try {
    if (cached === EMPTY) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, JSON.stringify(cached));
  } catch {
    // Private mode or full storage: the basket still works for this visit.
  }
  listeners.forEach((l) => l());
}

export function clearDeviceBasket(): void {
  setDeviceBasket(EMPTY);
}

/** Tests only: drop the in-memory copy so the next read comes from storage. */
// "Not now" on the save offer (BSK-11) lasts until the app reloads.
let offerDismissed = false;
export const saveOfferDismissed = (): boolean => offerDismissed;
export function dismissSaveOffer(): void {
  offerDismissed = true;
}

export function resetDeviceBasketForTests(): void {
  cached = null;
  offerDismissed = false;
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useDeviceBasket(): DeviceBasket {
  return useSyncExternalStore(subscribe, read, read);
}
