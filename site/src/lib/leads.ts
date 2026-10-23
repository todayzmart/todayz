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

/**
 * rejected: the API refused the details (400 or 422); the form says so distinctly.
 * rate_limited: 429. error: any other status (401, 403, 404, 405, 413, 415, 5xx …), which is the
 * site's or the API's fault, not the visitor's. network: no response, or none within the timeout.
 */
export type LeadResult = { ok: true } | { ok: false; reason: 'rejected' | 'rate_limited' | 'error' | 'network' };

/** How long the form waits for the API before it gives up and lets the visitor retry. */
export const LEAD_TIMEOUT_MS = 15_000;

export interface SubmitOptions {
  /** A ULID; the same key for every retry of the same details (see LeadKeys). */
  idempotencyKey: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** A ULID (48-bit millisecond time, 80 random bits, Crockford base32), as the API's Idempotency-Key expects. */
type RandomFill = (bytes: Uint8Array<ArrayBuffer>) => Uint8Array<ArrayBuffer>;

export function newIdempotencyKey(now: number = Date.now(), random: RandomFill = (b) => crypto.getRandomValues(b)): string {
  let time = '';
  let t = Math.floor(now);
  for (let i = 0; i < 10; i += 1) {
    time = CROCKFORD.charAt(t % 32) + time;
    t = Math.floor(t / 32);
  }
  const bytes = random(new Uint8Array(16));
  let rand = '';
  for (let i = 0; i < 16; i += 1) rand += CROCKFORD.charAt((bytes[i] ?? 0) % 32);
  return time + rand;
}

/**
 * One Idempotency-Key per form fill: the same details get the same key on every retry (a timeout
 * or a lost response must not create a second lead), changed details get a new key (the API
 * refuses a reused key with a different body), and a success starts a fresh fill.
 */
export class LeadKeys {
  #details = '';
  #key = '';
  readonly #make: () => string;

  constructor(make: () => string = () => newIdempotencyKey()) {
    this.#make = make;
  }

  keyFor(details: LeadInput): string {
    const fingerprint = JSON.stringify([details.name, details.shopName, details.phone, details.city]);
    if (fingerprint !== this.#details || this.#key === '') {
      this.#details = fingerprint;
      this.#key = this.#make();
    }
    return this.#key;
  }

  /** After a success: the next submission is a new lead. */
  reset(): void {
    this.#details = '';
    this.#key = '';
  }
}

export function leadStatus(status: number): LeadResult {
  if (status >= 200 && status < 300) return { ok: true };
  if (status === 400 || status === 422) return { ok: false, reason: 'rejected' };
  if (status === 429) return { ok: false, reason: 'rate_limited' };
  return { ok: false, reason: 'error' };
}

export async function submitLead(apiBase: string, request: LeadRequest, options: SubmitOptions): Promise<LeadResult> {
  const { idempotencyKey, fetchImpl = fetch, timeoutMs = LEAD_TIMEOUT_MS } = options;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetchImpl(`${apiBase.replace(/\/+$/, '')}/v1/public/leads`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json', 'idempotency-key': idempotencyKey },
      body: JSON.stringify(request),
      signal: controller.signal,
    });
    return leadStatus(response.status);
  } catch {
    return { ok: false, reason: 'network' };
  } finally {
    clearTimeout(timer);
  }
}
