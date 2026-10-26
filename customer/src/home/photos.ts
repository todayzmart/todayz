// Bundled CC0 stock photos for Home (D-249): the round category tiles, the static cards of the offers
// carousel and the fallback for an offer card whose banner has no image. Sources: photos/SOURCES.md.
import type { Tint } from '@todayz/ui';
import basket from './photos/basket.webp';
import beauty from './photos/beauty.webp';
import credits from './photos/credits.webp';
import electronics from './photos/electronics.webp';
import grocery from './photos/grocery.webp';
import hardware from './photos/hardware.webp';
import pharmacy from './photos/pharmacy.webp';
import shop from './photos/shop.webp';
import stationery from './photos/stationery.webp';

export const PHOTOS = { basket, credits, shop } as const;

const BY_SLUG: Record<string, string> = {
  grocery,
  pharmacy,
  beauty,
  electronics,
  mobiles: electronics,
  stationery,
  hardware,
};

/** The photo for a top-level category slug, or undefined (the tile keeps its icon). */
export function categoryPhoto(slug: string): string | undefined {
  return BY_SLUG[slug];
}

const BY_TINT: Record<Tint, string> = {
  green: grocery,
  rose: pharmacy,
  lilac: beauty,
  sky: electronics,
  amber: stationery,
  peach: hardware,
};

/** A photo that fits a shop's category tint, for offer cards without their own image. */
export function tintPhoto(tint: Tint | undefined): string {
  return BY_TINT[tint ?? 'green'];
}
