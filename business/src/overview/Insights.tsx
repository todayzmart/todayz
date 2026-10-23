import { DemandInsights, type InsightRow } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { toRow } from './insightRows';
import { RouterLink } from './RouterLink';
import styles from './overview.module.css';
import type { InsightData } from './types';

interface InsightsProps {
  insights: readonly InsightData[];
  onNotUseful: (row: InsightRow) => void;
}

/** Demand insights card (D-99): the kit card, laid out two by two on wide screens as on the design card. */
export function Insights({ insights, onNotUseful }: InsightsProps) {
  const intl = useIntl();
  return (
    <DemandInsights
      className={styles.insights}
      title={intl.formatMessage({ id: 'overview.insights.title' })}
      description={intl.formatMessage({ id: 'overview.insights.desc' })}
      periodTag={intl.formatMessage({ id: 'overview.insights.period' })}
      rows={insights.map((d) => toRow(intl, d))}
      onNotUseful={onNotUseful}
      notUsefulLabel={intl.formatMessage({ id: 'overview.insights.notUseful' })}
      allLink={{ label: intl.formatMessage({ id: 'overview.insights.all' }), href: '/analytics?tab=insights' }}
      footNote={intl.formatMessage({ id: 'overview.insights.foot' })}
      empty={{ title: intl.formatMessage({ id: 'overview.insights.empty' }) }}
      LinkComponent={RouterLink}
    />
  );
}
