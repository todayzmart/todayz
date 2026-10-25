import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import { isOpen } from './labels';

/** The five A-04 steps, mirroring B-02. */
export const STEPS = ['details', 'store', 'photos', 'items', 'owner'] as const;
export type Step = (typeof STEPS)[number];

export const STEP_LABEL: Record<Step, string> = {
  details: 'field.step.details',
  store: 'field.step.store',
  photos: 'field.step.photos',
  items: 'field.step.items',
  owner: 'field.step.owner',
};

export function stepDone(b: FieldBusiness, s: Step): boolean {
  switch (s) {
    case 'details':
      return !!b.categoryId;
    case 'store':
      return b.stores.length > 0;
    case 'photos':
      return b.photoCount > 0;
    case 'items':
      return b.stores.some((st) => st.itemCount > 0);
    case 'owner':
      return b.ownerConfirmed;
  }
}

/** Where a shop opens: the first step not done, or the items once it is with review. */
export function firstStep(b: FieldBusiness): Step {
  if (!isOpen(b.status)) return 'items';
  return STEPS.find((s) => !stepDone(b, s)) ?? 'owner';
}
