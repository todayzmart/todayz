import { text, ulid } from '../app/search';

/** A-23 view, kept in the URL: the tab (Gaps is the default) and one city. */
export interface DemandSearch {
  tab?: 'strip';
  city?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateDemandSearch(raw: Record<string, unknown>): DemandSearch {
  const out: DemandSearch = {};
  if (text(raw.tab) === 'strip') out.tab = 'strip';
  const city = ulid(raw.city);
  if (city) out.city = city;
  return out;
}
