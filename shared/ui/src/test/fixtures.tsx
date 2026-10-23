// Synthetic data for stories and tests: the names and numbers the design previews use
// (design/source/components/*/preview.html). No real customers or shops.
import type { LinkProps } from '../internal/link';
import type { QuantityStepperLabels } from '../QuantityStepper/QuantityStepper';

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
