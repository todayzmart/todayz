import type { AdminPulseRange } from '@todayz/api-client/gen/model/adminPulseRange';

export interface PulseSearch {
  /** Absent means the API's default, the last 7 days. */
  range?: Exclude<AdminPulseRange, '7d'>;
}

const RANGES = new Set(['today', '30d', '90d']);

export function validatePulseSearch(raw: Record<string, unknown>): PulseSearch {
  return typeof raw.range === 'string' && RANGES.has(raw.range) ? { range: raw.range as PulseSearch['range'] & string } : {};
}
