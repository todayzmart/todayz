// Shared data for the landing components (home and for-retailers). Text comes from message keys;
// only icons, tints, photos and example product names live here.
import type { ImageMetadata } from 'astro';
import beauty from '../../assets/photos/beauty.webp';
import electronics from '../../assets/photos/electronics.webp';
import grocery from '../../assets/photos/grocery.webp';
import hardware from '../../assets/photos/hardware.webp';
import pharmacy from '../../assets/photos/pharmacy.webp';
import stationery from '../../assets/photos/stationery.webp';
import type { IconName } from '../Icon.astro';

export type Tint = 'green' | 'amber' | 'rose' | 'sky' | 'lilac' | 'peach';

/** Shop types in home.types.1-6 order. The photo shows on the "For every kind of shop" cards (D-260; CC0, `assets/photos/SOURCES.md`). */
export const SHOP_TYPES: readonly { icon: IconName; tint: Tint; photo: ImageMetadata }[] = [
  { icon: 'basket', tint: 'green', photo: grocery },
  { icon: 'pill', tint: 'rose', photo: pharmacy },
  { icon: 'sparkles', tint: 'lilac', photo: beauty },
  { icon: 'pencil', tint: 'amber', photo: stationery },
  { icon: 'plug', tint: 'sky', photo: electronics },
  { icon: 'hammer', tint: 'peach', photo: hardware },
];

/** Splits a message that holds `{em}` into the text before and after it. */
export function splitEm(text: string): [string, string] {
  const [before = '', after = ''] = text.split('\u0000');
  return [before, after];
}

/** The `{em}` argument splitEm looks for. */
export const EM = { em: '\u0000' } as const;
