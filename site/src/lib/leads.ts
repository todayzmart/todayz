// Lead form logic: validation and the request to POST /v1/public/leads (Turnstile protected).
// Kept free of the DOM so it is unit-tested against a mocked fetch.
//
// TODO(W1-04): switch to the typed client from @todayz/api-client once the leads endpoint is in the
// contract. Until then this is a small typed fetch; field names follow data model `leads`
// (docs/plan/03-data-model.md) and the plan row POST /v1/public/leads (04-api.md).

export interface LeadInput {
  name: string;
  shop_name: string;
  phone: string;
  city: string;
}

export type LeadField = keyof LeadInput;

export interface LeadRequest extends LeadInput {
  /** Where the lead came from, for example "site:home:hi". */
  source: string;
  /** Cloudflare Turnstile token from the widget. */
  turnstile_token: string;
}

export type LeadValidation =
  | { ok: true; value: LeadInput }
  | { ok: false; errors: Partial<Record<LeadField, true>> };

/** A 10-digit Indian mobile number: strips spaces, dashes, a leading +91, 91 or 0. */
export function normalisePhone(raw: string): string | null {
  let digits = raw.replace(/[\s\-()]/g, '');
  if (digits.startsWith('+91')) digits = digits.slice(3);
  else if (digits.length === 12 && digits.startsWith('91')) digits = digits.slice(2);
  else if (digits.length === 11 && digits.startsWith('0')) digits = digits.slice(1);
  return /^[6-9]\d{9}$/.test(digits) ? digits : null;
}

export function validateLead(raw: Partial<Record<LeadField, string>>): LeadValidation {
  const name = (raw.name ?? '').trim();
  const shop = (raw.shop_name ?? '').trim();
  const city = (raw.city ?? '').trim();
  const phone = normalisePhone(raw.phone ?? '');
  const errors: Partial<Record<LeadField, true>> = {};
  if (name.length < 2 || name.length > 80) errors.name = true;
  if (shop.length < 2 || shop.length > 120) errors.shop_name = true;
  if (phone === null) errors.phone = true;
  if (city.length < 2 || city.length > 80) errors.city = true;
  if (Object.keys(errors).length > 0 || phone === null) return { ok: false, errors };
  return { ok: true, value: { name, shop_name: shop, phone, city } };
}

export type LeadResult = { ok: true } | { ok: false; reason: 'rejected' | 'rate_limited' | 'server' | 'network' };

export async function submitLead(
  apiBase: string,
  request: LeadRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<LeadResult> {
  let response: Response;
  try {
    response = await fetchImpl(`${apiBase.replace(/\/+$/, '')}/v1/public/leads`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json' },
      body: JSON.stringify(request),
    });
  } catch {
    return { ok: false, reason: 'network' };
  }
  if (response.ok) return { ok: true };
  if (response.status === 429) return { ok: false, reason: 'rate_limited' };
  if (response.status >= 500) return { ok: false, reason: 'server' };
  return { ok: false, reason: 'rejected' };
}
