// C-02: where the person shops from, kept on this device only (nothing is sent until search needs it).
import { browserStore } from '@todayz/i18n';
import { useSyncExternalStore } from 'react';

export type Place = { kind: 'gps'; lat: number; lng: number } | { kind: 'pin'; pin: string };

const KEY = 'todayz.place';
const listeners = new Set<() => void>();
let cache: { raw: string | null; value: Place | null } = { raw: null, value: null };

function parse(raw: string | null): Place | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw) as { kind?: unknown; lat?: unknown; lng?: unknown; pin?: unknown };
    if (v.kind === 'pin' && typeof v.pin === 'string' && /^[1-9][0-9]{5}$/.test(v.pin)) return { kind: 'pin', pin: v.pin };
    if (v.kind === 'gps' && typeof v.lat === 'number' && typeof v.lng === 'number') return { kind: 'gps', lat: v.lat, lng: v.lng };
  } catch {
    // fall through
  }
  return null;
}

export function getPlace(): Place | null {
  let raw: string | null = null;
  try {
    raw = browserStore()?.getItem(KEY) ?? null;
  } catch {
    raw = null;
  }
  if (raw !== cache.raw) cache = { raw, value: parse(raw) };
  return cache.value;
}

export function setPlace(place: Place): void {
  try {
    browserStore()?.setItem(KEY, JSON.stringify(place));
  } catch {
    // storage blocked: the choice lasts until reload
  }
  listeners.forEach((l) => l());
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function usePlace(): Place | null {
  return useSyncExternalStore(subscribe, getPlace, getPlace);
}
