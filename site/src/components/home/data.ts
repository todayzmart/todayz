// Shared data for the landing components (home and for-retailers). Text comes from message keys;
// only icons, tints and example product names live here.
import type { IconName } from '../Icon.astro';

export type Tint = 'green' | 'amber' | 'rose' | 'sky' | 'lilac' | 'peach';

/** Shop types in home.types.1-6 order. */
export const SHOP_TYPES: readonly { icon: IconName; tint: Tint }[] = [
  { icon: 'basket', tint: 'green' },
  { icon: 'pill', tint: 'rose' },
  { icon: 'sparkles', tint: 'lilac' },
  { icon: 'pencil', tint: 'amber' },
  { icon: 'plug', tint: 'sky' },
  { icon: 'hammer', tint: 'peach' },
];

/** Splits a message that holds `{em}` into the text before and after it. */
export function splitEm(text: string): [string, string] {
  const [before = '', after = ''] = text.split('\u0000');
  return [before, after];
}

/** The `{em}` argument splitEm looks for. */
export const EM = { em: '\u0000' } as const;
