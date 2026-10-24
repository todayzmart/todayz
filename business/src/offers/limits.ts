/**
 * What the offer and banner forms check before sending. The ranges are the contract's
 * (`backend/api/schemas/offers.yaml`, `banners.yaml`) and D-155's reading of the product rules; the
 * server is the gate and answers a 422 the form shows, so these only save a round trip and keep the
 * deep-link prefill inside what the backend accepts. The number of banners a shop may run
 * (`store_banners_max`) is a setting: the form sends and the server answers `banner_limit_reached`.
 */
export const OFFER_LIMITS = {
  /** A % off is a whole number in this range (D-155: 90 at most). */
  percentMin: 1,
  percentMax: 90,
  /** A flat ₹ off is per unit, at least 1 paise, and below the price of every picked item (the total never goes below ₹1). */
  flatMinPaise: 1,
  /** Buy X get Y: X and Y are whole numbers in these ranges. */
  buyMin: 1,
  buyMax: 20,
  getMin: 1,
  getMax: 20,
  /** A minimum-bill discount: the discount and the bill are in paise, the discount is below the bill. */
  minBillMinPaise: 1,
  maxRedemptionsMax: 1_000_000,
  titleMax: 40,
  /** Items one offer can name. */
  maxItems: 100,
} as const;

export const BANNER_LIMITS = {
  headlineMax: 40,
  /** TemplateBanner `subline`. */
  sublineMax: 60,
  /** Items a banner can point at. */
  maxItems: 20,
} as const;

/** True when `n` is a whole-number % inside the safe limits. */
export function percentInLimits(n: number): boolean {
  return Number.isInteger(n) && n >= OFFER_LIMITS.percentMin && n <= OFFER_LIMITS.percentMax;
}
