// Bundled CC0 and public-domain photos for Home. The `cat-` files are products on white for the pastel
// category tiles (D-269); the others are scenes for the static cards of the offers carousel (D-249) and
// the fallback for an offer card or shop cover without its own image. Sources: photos/SOURCES.md.
import type { Tint } from '@todayz/ui';
import basket from './photos/basket.webp';
import beauty from './photos/beauty.webp';
import catBeauty from './photos/cat-beauty.webp';
import catElectronics from './photos/cat-electronics.webp';
import catGrocery from './photos/cat-grocery.webp';
import catHardware from './photos/cat-hardware.webp';
import catPharmacy from './photos/cat-pharmacy.webp';
import catStationery from './photos/cat-stationery.webp';
import credits from './photos/credits.webp';
import electronics from './photos/electronics.webp';
import grocery from './photos/grocery.webp';
import hardware from './photos/hardware.webp';
import pharmacy from './photos/pharmacy.webp';
import shop from './photos/shop.webp';
import stationery from './photos/stationery.webp';

export const PHOTOS = { basket, credits, shop } as const;

const BY_SLUG: Record<string, string> = {
  grocery: catGrocery,
  pharmacy: catPharmacy,
  beauty: catBeauty,
  electronics: catElectronics,
  mobiles: catElectronics,
  stationery: catStationery,
  hardware: catHardware,
};

/** The product photo for a category slug, or undefined (the tile shows its tint only). */
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
