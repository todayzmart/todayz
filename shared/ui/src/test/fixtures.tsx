// Synthetic data for stories and tests: the names and numbers the design previews use
// (design/source/components/*/preview.html). No real customers or shops.
import type { LinkProps } from '../internal/link';
import type { QuantityStepperLabels } from '../QuantityStepper/QuantityStepper';
import { destination } from '../StoreMap/geo';
import type { MapShop } from '../StoreMap/StoreMap';

export const stepperLabels: QuantityStepperLabels = { add: 'Add', decrement: 'Remove one', increment: 'Add one' };

/** A router link stand-in that keeps navigation inside the story. */
export function StoryLink({ href, children, ...rest }: LinkProps) {
  return (
    <a
      href={href}
      {...rest}
      onClick={(e) => {
        e.preventDefault();
        rest.onClick?.(e);
      }}
    >
      {children}
    </a>
  );
}

/** Default card width when a design card does not set one. */
export const DESIGN_CARD_WIDTH = 720;

/**
 * Marks a story as the pixel twin of a design card: the visual suite screenshots it next to
 * design/source/components/<card>/preview.html at the card's size and compares them.
 */
export function designRef(card: string, height: number, width = DESIGN_CARD_WIDTH) {
  return {
    tags: ['design-ref'],
    parameters: { layout: 'fullscreen' as const, design: { card, width, height } },
  };
}

export const radiusOptions = [
  { km: 1, label: '1 km' },
  { km: 2, label: '2 km' },
  { km: 5, label: '5 km' },
  { km: 10, label: '10 km' },
  { km: 20, label: '20 km' },
] as const;

/** Jayanagar 4th Block, Bengaluru: the design's example neighbourhood. */
export const mapCenter = { lng: 77.5838, lat: 12.9279 };

const names = [
  'Sri Lakshmi Provision Stores',
  'Sharma General Store',
  'Sri Venkateshwara Medicals',
  'Nandini Milk Parlour',
  'Ganesh Stationery',
  'Raghavendra Hardware',
  'Fresh Basket',
  'Annapoorna Bakery',
  'Kaveri Electronics',
  'Mahalakshmi Fancy Store',
];

/** Deterministic synthetic shops: a tight cluster near the centre plus a scatter out to 4 km. */
export function makeShops(count: number): MapShop[] {
  const shops: MapShop[] = [];
  for (let i = 0; i < count; i++) {
    const near = i < 8;
    const km = near ? 0.15 + (i % 4) * 0.08 : 0.4 + ((i * 37) % 36) / 10;
    const p = destination(mapCenter, km * 1000, near ? 300 + i * 9 : (i * 137.5) % 360);
    shops.push({
      id: `shop-${i}`,
      name: `${names[i % names.length]}${i >= names.length ? ` ${Math.floor(i / names.length) + 1}` : ''}`,
      lng: p.lng,
      lat: p.lat,
      open: i % 7 !== 3,
    });
  }
  return shops;
}
