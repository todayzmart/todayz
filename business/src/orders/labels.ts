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

/**
 * "Amul Butter · 100 g" style line name: name, variant, then pack. Shops often type the pack into
 * the name ("Amul Butter 100 g", "Atta 5kg"); then the pack is not repeated.
 */
export function lineName(l: Pick<StoreOrderLine, 'name' | 'variant' | 'packSize' | 'unit'>): string {
  const pack = `${l.packSize} ${l.unit}`;
  const squash = (s: string) => s.replace(/\s+/g, '').toLowerCase();
  const named = squash(l.name).endsWith(squash(pack));
  return [l.name, l.variant, named ? null : pack].filter(Boolean).join(' · ');
}

/** The most the shop can give of a line: what was asked, capped by the stock on hand. */
export function canGive(l: Pick<StoreOrderLine, 'available' | 'quantityRequested'>): number {
  return Math.min(l.quantityRequested, Math.max(0, l.available));
}

/** Whether Accept part can give anything: some stock on a line, and more than one unit asked in all. */
export function canAcceptPart(lines: readonly Pick<StoreOrderLine, 'available' | 'quantityRequested'>[]): boolean {
  const asked = lines.reduce((n, l) => n + l.quantityRequested, 0);
  return asked > 1 && lines.some((l) => canGive(l) > 0);
}

export function isShort(l: Pick<StoreOrderLine, 'available' | 'quantityRequested'>): boolean {
  return l.available < l.quantityRequested;
}
