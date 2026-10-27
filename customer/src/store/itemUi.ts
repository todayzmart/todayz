// Small helpers shared by the item card and the product page.
import type { QuantityStepperLabels } from '@todayz/ui';
import { useIntl } from 'react-intl';

export function useStepperLabels(): QuantityStepperLabels {
  const intl = useIntl();
  return {
    add: intl.formatMessage({ id: 'product.add' }),
    decrement: intl.formatMessage({ id: 'product.remove' }),
    increment: intl.formatMessage({ id: 'product.addOne' }),
    max: (max) => intl.formatMessage({ id: 'product.max' }, { max: String(max) }),
  };
}

export function itemHref(shopSlug: string, storeItemId: string): string {
  return `/s/${encodeURIComponent(shopSlug)}/i/${encodeURIComponent(storeItemId)}`;
}

const TINTS = ['green', 'sky', 'amber', 'lilac', 'peach', 'rose'] as const;

/** A pastel per product that stays the same wherever the product shows. */
export function tintOf(id: string): (typeof TINTS)[number] {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return TINTS[h % TINTS.length] ?? 'green';
}
