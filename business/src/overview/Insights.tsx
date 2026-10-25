import { DemandInsights, useToast, type InsightRow } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
import { useCallback, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { toRow } from './insightRows';
import { dismissInsight, useInsightCard } from './queries';
import { RouterLink } from './RouterLink';
import { SetupChecklist } from './SetupCard';
import styles from './overview.module.css';
import type { InsightData, SetupProgress } from './types';

interface InsightsProps {
  store: CurrentStore;
  setup: SetupProgress;
}

/**
 * Demand insights card (D-99): the API's top 4 rows, laid out two by two on wide screens as on the
 * design card. "Not useful" hides the row at once and quiets its subject on the server; the next
 * row moves up when the card reloads. With nothing firing, the card lists the setup steps left.
 */
export function Insights({ store, setup }: InsightsProps) {
  const intl = useIntl();
  const toast = useToast();
  const qc = useQueryClient();
  const card = useInsightCard(store);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const items = card.data?.items ?? [];
  const byId = new Map<string, InsightData>(items.map((i) => [i.id, i]));

  const onNotUseful = useCallback(
    (row: InsightRow) => {
      const d = byId.get(row.id);
      if (!d?.subjectRef) return;
      setHidden((h) => new Set(h).add(row.id));
      dismissInsight(qc, store, d.kind, d.subjectRef).then(
        (until) =>
          toast.show({
            message: intl.formatMessage(
              { id: 'overview.insights.hiddenUntil' },
              { date: intl.formatDate(new Date(until), { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' }) },
            ),
          }),
        () => {
          setHidden((h) => {
            const n = new Set(h);
            n.delete(row.id);
            return n;
          });
          toast.show({ icon: CircleAlert, message: intl.formatMessage({ id: 'overview.insights.dismissFailed' }) });
        },
      );
    },
    // byId is rebuilt every render from the same answer; the row id is what matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [card.data, intl, qc, store, toast],
  );

  const rows = items
    .filter((i) => !hidden.has(i.id))
    .map((d) => toRow(intl, d))
    .filter((r): r is InsightRow => r !== null);
  const days = card.data?.period.days;
  return (
    <DemandInsights
      className={styles.insights}
      title={intl.formatMessage({ id: 'overview.insights.title' })}
      description={intl.formatMessage({ id: 'overview.insights.desc' })}
      periodTag={days ? intl.formatMessage({ id: 'overview.insights.period' }, { days }) : undefined}
      rows={rows}
      loading={card.isPending}
      onNotUseful={onNotUseful}
      notUsefulLabel={intl.formatMessage({ id: 'overview.insights.notUseful' })}
      allLink={{ label: intl.formatMessage({ id: 'overview.insights.all' }), href: '/analytics?tab=insights' }}
      footNote={intl.formatMessage({ id: 'overview.insights.foot' })}
      empty={{
        title: card.isError
          ? intl.formatMessage({ id: 'overview.insights.error' })
          : intl.formatMessage({ id: 'overview.insights.empty' }),
        checklist: card.isError ? undefined : <SetupChecklist setup={setup} />,
      }}
      LinkComponent={RouterLink}
    />
  );
}
