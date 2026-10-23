import type { StoreOrderLine } from '@todayz/api-client/gen/model/storeOrderLine';
import { formatClock } from '@todayz/i18n';
import type { OrderStatus } from '@todayz/ui';
import type { IntlShape } from 'react-intl';

export function statusLabel(intl: IntlShape, status: OrderStatus): string {
  return intl.formatMessage({ id: `orders.status.${status}` });
}

/** Why an order ended, in words; unknown reasons fall back to the raw code's "other" wording. */
export function reasonLabel(intl: IntlShape, reason: string): string {
  const id = `orders.reason.${reason}`;
  return intl.messages[id] ? intl.formatMessage({ id }) : intl.formatMessage({ id: 'orders.reason.other' });
}

/** "6:40 pm" in IST. */
export function clock(iso: string): string {
  return formatClock(iso);
}

/** "Amul Butter · 100 g" style line name: name, variant, then pack. */
export function lineName(l: Pick<StoreOrderLine, 'name' | 'variant' | 'packSize' | 'unit'>): string {
  return [l.name, l.variant, `${l.packSize} ${l.unit}`].filter(Boolean).join(' · ');
}

export function isShort(l: Pick<StoreOrderLine, 'available' | 'quantityRequested'>): boolean {
  return l.available < l.quantityRequested;
}
