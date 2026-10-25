import { InsightKind } from '@todayz/api-client/gen/model/insightKind';

export type AnalyticsTab = 'sales' | 'insights';

export interface AnalyticsSearch {
  tab?: AnalyticsTab;
  /** One insight kind for the Insights tab's filter. */
  kind?: InsightKind;
}

const KINDS = new Set<string>(Object.values(InsightKind));

/** `/analytics?tab=insights&kind=…`: B-03's "All insights" link lands here. Anything else is dropped. */
export function validateAnalyticsSearch(raw: Record<string, unknown>): AnalyticsSearch {
  const out: AnalyticsSearch = {};
  if (raw.tab === 'sales' || raw.tab === 'insights') out.tab = raw.tab;
  if (typeof raw.kind === 'string' && KINDS.has(raw.kind)) out.kind = raw.kind as InsightKind;
  return out;
}
