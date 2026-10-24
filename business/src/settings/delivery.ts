import type { StoreSettings } from '@todayz/api-client/gen/model/storeSettings';
import type { StoreSettingsUpdate } from '@todayz/api-client/gen/model/storeSettingsUpdate';

/** Cashback is 0 to 20% in 0.5% steps (`cashback_bp` 0–2000, step 50; 01 §6). */
export const CASHBACK_STEP_BP = 50;
export const CASHBACK_MAX_BP = 2000;
export const CASHBACK_OPTIONS: readonly number[] = Array.from({ length: CASHBACK_MAX_BP / CASHBACK_STEP_BP + 1 }, (_, i) => i * CASHBACK_STEP_BP);

/** Credits a customer earns on a bill: `floor(base_paise × cashback_bp / 1 000 000)` (01 §6). */
export const cashbackCredits = (basePaise: number, bp: number): number => Math.floor((basePaise * bp) / 1_000_000);

/** Rupees typed as text → paise, or null when it is not a whole-paise amount. */
export function rupeesToPaise(text: string): number | null {
  const t = text.trim().replace(/,/g, '');
  if (!/^\d+(\.\d{1,2})?$/.test(t)) return null;
  return Math.round(Number(t) * 100);
}

export const paiseToRupees = (paise: number): string => (paise % 100 === 0 ? String(paise / 100) : (paise / 100).toFixed(2));

/** Kilometres typed as text → metres (the API's unit), or null. */
export function kmToMetres(text: string): number | null {
  const t = text.trim();
  if (!/^\d+(\.\d{1,3})?$/.test(t)) return null;
  return Math.round(Number(t) * 1000);
}

export const metresToKm = (m: number): string => String(m / 1000);

/** The API's bounds for these three numbers (`StoreSettingsUpdate`). The server remains the judge. */
export const RADIUS_MIN_M = 100;
export const RADIUS_MAX_M = 20_000;
export const MONEY_MAX_PAISE = 100_000_000;

export interface DeliveryValues {
  pickup: boolean;
  delivery: boolean;
  radiusKm: string;
  feeRupees: string;
  freeAboveRupees: string;
  minOrderRupees: string;
}

export const deliveryValuesOf = (s: StoreSettings): DeliveryValues => ({
  pickup: s.acceptsPickup,
  delivery: s.acceptsDelivery,
  radiusKm: metresToKm(s.deliveryRadiusM),
  feeRupees: paiseToRupees(s.deliveryFeePaise),
  freeAboveRupees: s.deliveryFreeAbovePaise === null ? '' : paiseToRupees(s.deliveryFreeAbovePaise),
  minOrderRupees: paiseToRupees(s.deliveryMinOrderPaise),
});

/** D-41: with neither pickup nor delivery on, the shop shows as in-store only. */
export const inStoreOnly = (v: Pick<DeliveryValues, 'pickup' | 'delivery'>): boolean => !v.pickup && !v.delivery;

export type DeliveryErrors = Partial<Record<'radiusKm' | 'feeRupees' | 'freeAboveRupees' | 'minOrderRupees', 'radius' | 'money'>>;

/** Delivery terms only matter (and are only checked) while delivery is on. */
export function validateDelivery(v: DeliveryValues): DeliveryErrors {
  const e: DeliveryErrors = {};
  if (!v.delivery) return e;
  const m = kmToMetres(v.radiusKm);
  if (m === null || m < RADIUS_MIN_M || m > RADIUS_MAX_M) e.radiusKm = 'radius';
  const money = (t: string) => {
    const p = rupeesToPaise(t);
    return p !== null && p <= MONEY_MAX_PAISE;
  };
  if (!money(v.feeRupees)) e.feeRupees = 'money';
  if (!money(v.minOrderRupees)) e.minOrderRupees = 'money';
  if (v.freeAboveRupees.trim() !== '') {
    const p = rupeesToPaise(v.freeAboveRupees);
    if (p === null || p < 1 || p > MONEY_MAX_PAISE) e.freeAboveRupees = 'money';
  }
  return e;
}

/** What to send: the two switches always, the terms only when delivery is on and they changed. */
export function deliveryPayload(v: DeliveryValues, before: DeliveryValues): StoreSettingsUpdate {
  const out: StoreSettingsUpdate = {};
  if (v.pickup !== before.pickup) out.acceptsPickup = v.pickup;
  if (v.delivery !== before.delivery) out.acceptsDelivery = v.delivery;
  if (v.delivery) {
    if (v.radiusKm !== before.radiusKm) out.deliveryRadiusM = kmToMetres(v.radiusKm) ?? 0;
    if (v.feeRupees !== before.feeRupees) out.deliveryFeePaise = rupeesToPaise(v.feeRupees) ?? 0;
    if (v.minOrderRupees !== before.minOrderRupees) out.deliveryMinOrderPaise = rupeesToPaise(v.minOrderRupees) ?? 0;
    if (v.freeAboveRupees !== before.freeAboveRupees) out.deliveryFreeAbovePaise = v.freeAboveRupees.trim() === '' ? null : (rupeesToPaise(v.freeAboveRupees) ?? 0);
  }
  return out;
}
