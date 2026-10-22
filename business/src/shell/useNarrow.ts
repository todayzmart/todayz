import { useSyncExternalStore } from 'react';

/** Below this width the business app shows bottom tabs and touch-sized controls (Sidebar README). */
export const NARROW_QUERY = '(width < 1024px)';

function subscribe(onChange: () => void) {
  const mql = window.matchMedia(NARROW_QUERY);
  mql.addEventListener('change', onChange);
  return () => mql.removeEventListener('change', onChange);
}

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
