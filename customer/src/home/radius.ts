// The search radius chips (01 §11: 1, 2, 5, 10, 20 km, default 2). Kept on this device so Home, Search
// and the shop page agree. TODO(lead): the options are a constant here because no public settings
// endpoint exists; they match the `radius` enum of the discover contract.
import { browserStore } from '@todayz/i18n';
import { useSyncExternalStore } from 'react';

export const RADIUS_OPTIONS_KM = [1, 2, 5, 10, 20] as const;
export const DEFAULT_RADIUS_KM = 2;
const KEY = 'todayz.radiusKm';
const listeners = new Set<() => void>();

export function getRadiusKm(): number {
  let raw: string | null = null;
  try {
    raw = browserStore()?.getItem(KEY) ?? null;
  } catch {
    raw = null;
  }
  const n = Number(raw);
  return (RADIUS_OPTIONS_KM as readonly number[]).includes(n) ? n : DEFAULT_RADIUS_KM;
}

export function setRadiusKm(km: number): void {
  try {
    browserStore()?.setItem(KEY, String(km));
  } catch {
    // storage blocked: the choice lasts until reload
  }
  listeners.forEach((l) => l());
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useRadiusKm(): number {
  return useSyncExternalStore(subscribe, getRadiusKm, getRadiusKm);
}
