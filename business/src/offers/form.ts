/**
 * The offer form's draft, its checks against the limits (`limits.ts`) and the request bodies it
 * becomes. Pure: the screen shows the error codes as `offers.form.error.<code>` messages. The server
 * stays the gate; these checks only save a round trip.
 *
 * D-155: a percent offer is 1 to 90; a flat offer is per unit; a minimum-bill discount covers the
 * whole shop; the terms of a saved offer never change (only pause, resume and end).
 */
import type { OfferChannels } from '@todayz/api-client/gen/model/offerChannels';
import type { OfferCreate } from '@todayz/api-client/gen/model/offerCreate';
import type { OfferScope } from '@todayz/api-client/gen/model/offerScope';
import { parseRupees } from '../stock/format';
import { OFFER_LIMITS, percentInLimits } from './limits';

/** What the owner picks. A flash offer is a % off one expiring batch. */
export type OfferChoice = 'percent' | 'flat' | 'bxgy' | 'bill_min' | 'flash';

export interface PickedItem {
  id: string;
  title: string;
  pricePaise: number;
}

/** The part of a stock batch the flash form needs. */
export interface ExpiringBatch {
  id: string;
  itemId: string;
  itemName: string;
  expiresOn: string | null;
  quantity: number;
}

export interface OfferDraft {
  choice: OfferChoice;
  /** Used by `percent` and `flat`; the other kinds fix it. */
  scope: OfferScope;
  items: PickedItem[];
  categoryId: string;
  batchIds: string[];
  /** `percent` and `flash`: whole percent. `flat` and `bill_min`: rupees. */
  value: string;
  buyQty: string;
  getQty: string;
  minBill: string;
  title: string;
  channels: OfferChannels;
  oncePerCustomer: boolean;
  /** "First N orders"; empty = no limit. */
  maxRedemptions: string;
  /** `datetime-local` text; empty start = now. A flash offer ends with its batch, so it has neither. */
  startsAt: string;
  endsAt: string;
}

export function emptyDraft(over: Partial<OfferDraft> = {}): OfferDraft {
  return {
    choice: 'percent',
    scope: 'items',
    items: [],
    categoryId: '',
    batchIds: [],
    value: '',
    buyQty: '',
    getQty: '',
    minBill: '',
    title: '',
    channels: 'both',
    oncePerCustomer: false,
    maxRedemptions: '',
    startsAt: '',
    endsAt: '',
    ...over,
  };
}

/** The scope a choice forces: flash and buy X get Y name items, a minimum-bill discount is the whole shop. */
export function scopeOf(d: Pick<OfferDraft, 'choice' | 'scope'>): OfferScope {
  if (d.choice === 'flash' || d.choice === 'bxgy') return 'items';
  if (d.choice === 'bill_min') return 'store';
  return d.scope;
}

export type FieldKey = 'value' | 'items' | 'category' | 'batches' | 'buy' | 'get' | 'minBill' | 'maxRedemptions' | 'title' | 'startsAt' | 'endsAt';

/** An error: a message id suffix and the numbers it quotes. */
export interface FieldError {
  code: string;
  values?: Record<string, number | string>;
}

export type Errors = Partial<Record<FieldKey, FieldError>>;

export interface Context {
  batches: readonly ExpiringBatch[];
  /** Epoch ms; the device clock is enough for a form check (the server decides). */
  now: number;
}

export interface Validation {
  errors: Errors;
  /** Empty while anything is wrong. A flash draft makes one offer per ticked batch (one open flash offer per batch). */
  bodies: OfferCreate[];
}

const whole = (text: string): number | null => (/^\d{1,9}$/.test(text.trim()) ? Number(text.trim()) : null);

/** `datetime-local` text as a Date (device time zone); null for empty or unreadable. */
export function parseLocal(text: string): Date | null {
  if (!text) return null;
  const d = new Date(text);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** A start this far in the past is a mistake rather than "now". */
const START_SLACK_MS = 5 * 60_000;

/** Checks a start/end pair; both are optional, and the end must come after the start. */
export function checkSchedule(
  startText: string,
  endText: string,
  now: number,
): { errors: Pick<Errors, 'startsAt' | 'endsAt'>; startsAt?: string; endsAt?: string } {
  const errors: Pick<Errors, 'startsAt' | 'endsAt'> = {};
  const start = parseLocal(startText);
  const end = parseLocal(endText);
  if (startText && !start) errors.startsAt = { code: 'dateInvalid' };
  else if (start && start.getTime() < now - START_SLACK_MS) errors.startsAt = { code: 'startPast' };
  if (endText && !end) errors.endsAt = { code: 'dateInvalid' };
  else if (end && end.getTime() <= (start ? start.getTime() : now)) errors.endsAt = { code: 'endBeforeStart' };
  return {
    errors,
    ...(start && !errors.startsAt ? { startsAt: start.toISOString() } : {}),
    ...(end && !errors.endsAt ? { endsAt: end.toISOString() } : {}),
  };
}

/** The batches a flash draft covers: those of its ticked ids that the list knows. */
export function flashBatches(d: Pick<OfferDraft, 'batchIds'>, batches: readonly ExpiringBatch[]): ExpiringBatch[] {
  const ticked = new Set(d.batchIds);
  return batches.filter((b) => ticked.has(b.id));
}

export function validateDraft(d: OfferDraft, ctx: Context): Validation {
  const errors: Errors = {};
  const scope = scopeOf(d);
  const body: OfferCreate = { kind: 'percent', scope, channels: d.channels };
  let lowestPrice: number | null = null;
  let picked: ExpiringBatch[] = [];

  if (d.choice === 'flash') {
    picked = flashBatches(d, ctx.batches);
    if (picked.length === 0) errors.batches = { code: 'batchesRequired' };
  } else if (scope === 'items') {
    if (d.items.length === 0) errors.items = { code: 'itemsRequired' };
    else if (d.choice === 'bxgy' && d.items.length !== 1) errors.items = { code: 'oneItem' };
    else if (d.items.length > OFFER_LIMITS.maxItems) errors.items = { code: 'tooManyItems', values: { max: OFFER_LIMITS.maxItems } };
    else body.storeItemIds = d.items.map((i) => i.id);
    lowestPrice = d.items.length > 0 ? Math.min(...d.items.map((i) => i.pricePaise)) : null;
  } else if (scope === 'category') {
    if (!d.categoryId) errors.category = { code: 'categoryRequired' };
    else body.categoryId = d.categoryId;
  }

  if (d.choice === 'percent' || d.choice === 'flash') {
    const n = whole(d.value);
    if (n === null || !percentInLimits(n)) errors.value = { code: 'percent', values: { min: OFFER_LIMITS.percentMin, max: OFFER_LIMITS.percentMax } };
    else {
      body.kind = 'percent';
      body.value = n;
    }
  } else if (d.choice === 'flat' || d.choice === 'bill_min') {
    const paise = parseRupees(d.value);
    if (paise === null || paise < OFFER_LIMITS.flatMinPaise) errors.value = { code: 'flatMin' };
    else if (d.choice === 'flat' && lowestPrice !== null && paise >= lowestPrice) errors.value = { code: 'flatTooHigh' };
    else {
      body.kind = d.choice;
      body.value = paise;
    }
  }

  if (d.choice === 'bxgy') {
    body.kind = 'bxgy';
    const buy = whole(d.buyQty);
    const get = whole(d.getQty);
    if (buy === null || buy < OFFER_LIMITS.buyMin || buy > OFFER_LIMITS.buyMax) errors.buy = { code: 'buy', values: { min: OFFER_LIMITS.buyMin, max: OFFER_LIMITS.buyMax } };
    else body.buyQty = buy;
    if (get === null || get < OFFER_LIMITS.getMin || get > OFFER_LIMITS.getMax) errors.get = { code: 'get', values: { min: OFFER_LIMITS.getMin, max: OFFER_LIMITS.getMax } };
    else body.getQty = get;
  }

  if (d.choice === 'bill_min') {
    const min = parseRupees(d.minBill);
    const off = parseRupees(d.value);
    if (min === null || min < OFFER_LIMITS.minBillMinPaise) errors.minBill = { code: 'minBill' };
    else if (off !== null && off >= min) errors.minBill = { code: 'discountAboveBill' };
    else body.minBillPaise = min;
  }

  if (d.title.trim().length > OFFER_LIMITS.titleMax) errors.title = { code: 'title', values: { max: OFFER_LIMITS.titleMax } };
  else if (d.title.trim()) body.title = d.title.trim();

  if (d.maxRedemptions.trim() !== '') {
    const n = whole(d.maxRedemptions);
    if (n === null || n < 1 || n > OFFER_LIMITS.maxRedemptionsMax) errors.maxRedemptions = { code: 'maxRedemptions', values: { max: OFFER_LIMITS.maxRedemptionsMax } };
    else body.maxRedemptions = n;
  }
  if (d.oncePerCustomer) body.perCustomerLimit = 1;

  if (d.choice !== 'flash') {
    const sched = checkSchedule(d.startsAt, d.endsAt, ctx.now);
    Object.assign(errors, sched.errors);
    if (sched.startsAt) body.startsAt = sched.startsAt;
    if (sched.endsAt) body.endsAt = sched.endsAt;
  }

  if (Object.keys(errors).length > 0) return { errors, bodies: [] };
  if (d.choice === 'flash') {
    return { errors, bodies: picked.map((b) => ({ ...body, storeItemIds: [b.itemId], batchId: b.id })) };
  }
  return { errors, bodies: [body] };
}
