import { useSyncExternalStore } from 'react';

/** Below this width the business app shows bottom tabs and touch-sized controls (Sidebar README). */
export const NARROW_QUERY = '(width < 1024px)';

/** Phones only (D-261): below this width a wide table becomes a list with one card row per item. */
export const PHONE_QUERY = '(width < 640px)';

function subscriber(query: string) {
  return (onChange: () => void) => {
    const mql = window.matchMedia(query);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  };
}

const subscribe = subscriber(NARROW_QUERY);
const subscribePhone = subscriber(PHONE_QUERY);

/**
 * True on phones and tablets. The content area then gets `data-density="touch"` so buttons,
 * inputs and chips are 44px targets (design README, "touch density").
 */
export function useNarrow(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(NARROW_QUERY).matches,
    () => true,
  );
}

/** True on phones (under 640px), where a table would scroll sideways. */
export function usePhone(): boolean {
  return useSyncExternalStore(
    subscribePhone,
    () => window.matchMedia(PHONE_QUERY).matches,
    () => true,
  );
}
