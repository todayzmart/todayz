import { formatDistance } from '@todayz/i18n';
import type { IntlShape } from 'react-intl';
import type { InsightData } from '../overview/types';

type Bag = Readonly<Record<string, unknown>>;
const num = (b: Bag | undefined, k: string): number => {
  const v = b?.[k];
  return typeof v === 'number' && Number.isFinite(v) ? v : 0;
};

/** The muted "what data it needs" line of a kind below its minimum evidence (D-99, tab only). */
export function needsLine(intl: IntlShape, d: InsightData): string {
  const p = d.evidence.needs?.params as Bag | undefined;
  switch (d.kind) {
    case 'selling_fast':
      return intl.formatMessage({ id: 'analytics.insights.needs.selling_fast' }, { units: num(p, 'minUnits') });
    case 'sitting_on_shelf':
      return intl.formatMessage({ id: 'analytics.insights.needs.sitting_on_shelf' }, { days: num(p, 'minDaysListed') });
    case 'wanted_nearby':
      return intl.formatMessage(
        { id: 'analytics.insights.needs.wanted_nearby' },
        { searches: num(p, 'minSearches'), distance: formatDistance(num(p, 'radiusM')) },
      );
    case 'missed_sales':
      return intl.formatMessage({ id: 'analytics.insights.needs.missed_sales' }, { searches: num(p, 'minSearches') });
    case 'bought_together':
      return intl.formatMessage({ id: 'analytics.insights.needs.bought_together' }, { bills: num(p, 'minBills') });
    case 'regulars_slipping':
      return intl.formatMessage({ id: 'analytics.insights.needs.regulars_slipping' });
    case 'priced_above_nearby':
      return intl.formatMessage({ id: 'analytics.insights.needs.priced_above_nearby' }, { stores: num(p, 'minStores') });
    default:
      return intl.formatMessage({ id: 'analytics.insights.needs.other' });
  }
}
