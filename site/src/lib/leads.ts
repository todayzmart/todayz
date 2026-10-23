// Lead form logic: validation and the request to POST /v1/public/leads (Turnstile protected).
// Kept free of the DOM so it is unit-tested against a mocked fetch.
//
// TODO(W1-04): switch to the typed client from @todayz/api-client once the leads endpoint is in the
// contract. Until then this is a small typed fetch. The payload is camelCase like the rest of the
// API (name, shopName, phone, city, source, turnstileToken) and the phone is E.164 (+91XXXXXXXXXX);
// both are an open question for W1-04 (docs/qa/W1-09/REPORT.md).

export interface LeadInput {
  name: string;
  shopName: string;
  /** E.164, always +91 followed by 10 digits. */
  phone: string;
  city: string;
}

export type LeadField = keyof LeadInput;

export const LEAD_FIELDS: readonly LeadField[] = ['name', 'phone', 'shopName', 'city'];

export interface LeadRequest extends LeadInput {
  /** Where the lead came from, for example "site:home:hi". */
  source: string;
  /** Cloudflare Turnstile token from the widget. */
  turnstileToken: string;
}

export type LeadValidation =
  | { ok: true; value: LeadInput }
  | { ok: false; errors: Partial<Record<LeadField, true>> };

/**
 * An Indian mobile number as E.164 (+91XXXXXXXXXX), or null. Accepts spaces, dashes, dots and
 * brackets, one country prefix (+91, 0091, or 91 before ten digits) and one trunk 0, so
 * "0091 90000 00012" and "+91 09000000012" both become +919000000012. Any other country code is
 * rejected: the product serves Indian numbers only.
 */
export function normalisePhone(raw: string): string | null {
  let digits = raw.replace(/[\s\-().]/g, '');
  if (digits.startsWith('+')) {
    if (!digits.startsWith('+91')) return null;
    digits = digits.slice(3);
  } else if (digits.startsWith('0091')) {
    digits = digits.slice(4);
  } else if (digits.length === 12 && digits.startsWith('91')) {
    digits = digits.slice(2);
  }
  if (digits.length === 11 && digits.startsWith('0')) digits = digits.slice(1);
  return /^[6-9]\d{9}$/.test(digits) ? `+91${digits}` : null;
}

export function validateLead(raw: Partial<Record<LeadField, string>>): LeadValidation {
  const name = (raw.name ?? '').trim();
  const shopName = (raw.shopName ?? '').trim();
  const city = (raw.city ?? '').trim();
  const phone = normalisePhone(raw.phone ?? '');
  const errors: Partial<Record<LeadField, true>> = {};
  if (name.length < 2 || name.length > 80) errors.name = true;
  if (phone === null) errors.phone = true;
  if (shopName.length < 2 || shopName.length > 120) errors.shopName = true;
  if (city.length < 2 || city.length > 80) errors.city = true;
  if (Object.keys(errors).length > 0 || phone === null) return { ok: false, errors };
  return { ok: true, value: { name, shopName, phone, city } };
}

/** rejected: the API refused the details (4xx other than 429); the form says so distinctly. */
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
