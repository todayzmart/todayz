import { ApiError } from '@todayz/api-client';

/** What a failed AI call means for the person, as an `ai.error.<key>` suffix. */
export type AiErrorKey = 'cap' | 'busy' | 'off' | 'changed' | 'photo' | 'invalid' | 'offline' | 'generic';

export function aiErrorKey(err: unknown): AiErrorKey {
  if (!(err instanceof ApiError)) return 'offline';
  if (err.code === 'ai_daily_cap_reached') return 'cap';
  // Too many photos still being read for the store (429): wait and try again.
  if (err.code === 'rate_limited') return 'busy';
  // A feature switched off answers the common 404 (11 §1), so the entry point goes away.
  if (err.code === 'not_found') return 'off';
  if (err.code === 'invalid_transition' || err.code === 'conflict') return 'changed';
  if (err.status === 413 || err.status === 415) return 'photo';
  if (err.code === 'validation_failed') return 'invalid';
  return 'generic';
}

/** The minutes until the cap lifts, from `Retry-After` (rounded up), or null when the server gave none. */
export function retryMinutes(err: unknown): number | null {
  if (!(err instanceof ApiError) || err.retryAfterSeconds === undefined) return null;
  return Math.max(1, Math.ceil(err.retryAfterSeconds / 60));
}

/**
 * The field reasons of a 422 per line: `body.lines[2].value.costPaise: above_mrp` becomes
 * `{2: {costPaise: 'above_mrp'}}`. A reason on the line itself (`body.lines[1].outcome`) is under `outcome`.
 */
export function lineReasons(err: unknown): Record<number, Record<string, string>> {
  const out: Record<number, Record<string, string>> = {};
  if (!(err instanceof ApiError)) return out;
  for (const [path, reason] of Object.entries(err.fields)) {
    const m = /^body\.lines\[(\d+)\](?:\.value)?\.(\w+)$/.exec(path);
    if (!m) continue;
    const i = Number(m[1]);
    (out[i] ??= {})[m[2] as string] = reason;
  }
  return out;
}

/** A field reason outside the lines (`body.reviewId`, `body.itemIds`), or null. */
export function bodyReason(err: unknown, field: string): string | null {
  if (!(err instanceof ApiError)) return null;
  return err.fields[`body.${field}`] ?? null;
}

/** The line reasons the confirm endpoints answer (billscan, shelfscan), plus the two the browser finds first. */
const LINE_REASONS = new Set(['range', 'above_mrp', 'past', 'date', 'required', 'not_in_store', 'invalid', 'no_product', 'duplicate', 'already_listed', 'quantity', 'price']);

/** The message id for a line reason; an unknown one reads as "check this line". */
export const lineReasonId = (reason: string) => `ai.err.${LINE_REASONS.has(reason) ? reason : 'invalid'}`;

/** The line reason each per-line refusal of a confirm reads as (AiLineRefusal). */
const REFUSALS: Record<string, string> = {
  not_permitted: 'not_permitted',
  invalid_item: 'not_in_store',
  invalid_quantity: 'range',
  invalid_cost: 'range',
  invalid_price: 'range',
  invalid_opening_stock: 'range',
  invalid_cost_above_mrp: 'above_mrp',
  invalid_price_above_mrp: 'above_mrp',
  invalid_expiry: 'past',
  invalid_product: 'no_product',
  invalid_listed: 'already_listed',
  invalid_duplicate: 'duplicate',
};

/** The message id for a refused line's reason; an unknown one reads as "check this line". */
export const refusalId = (reason: string | undefined) => `ai.err.${REFUSALS[reason ?? ''] ?? 'invalid'}`;
