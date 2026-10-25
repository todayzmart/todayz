/** Shared parsing for filters kept in the URL: malformed values are dropped, never sent to the API. */

export const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const DAY = /^\d{4}-\d{2}-\d{2}$/;

export const text = (v: unknown) => (typeof v === 'string' || typeof v === 'number' ? String(v).trim() : '');

/** A ULID, upper-cased, or undefined. */
export function ulid(v: unknown): string | undefined {
  const id = text(v).toUpperCase();
  return ULID.test(id) ? id : undefined;
}

/** An IST calendar day (YYYY-MM-DD), or undefined. */
export function day(v: unknown): string | undefined {
  const d = text(v);
  return DAY.test(d) && !Number.isNaN(Date.parse(`${d}T00:00:00+05:30`)) ? d : undefined;
}

/** IST midnight starting the day. */
export const dayStart = (d: string) => new Date(`${d}T00:00:00+05:30`).toISOString();

/** The next IST midnight: the API's `to` is exclusive, so a day runs up to it. */
export const dayEnd = (d: string) => new Date(Date.parse(`${d}T00:00:00+05:30`) + 86_400_000).toISOString();

/** A `datetime-local` value (YYYY-MM-DDTHH:mm) read as IST, as an instant; null if malformed. */
export function istInstant(v: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(v)) return null;
  const t = Date.parse(`${v}:00+05:30`);
  return Number.isNaN(t) ? null : new Date(t).toISOString();
}
