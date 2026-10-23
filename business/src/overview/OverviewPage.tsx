import { Banner, Button, Skeleton, ToastProvider, useToast, VisuallyHidden, type InsightRow } from '@todayz/ui';
import { useCallback, useState } from 'react';
import { useIntl } from 'react-intl';
import { CoachSlot } from './CoachSlot';
import { GreetBand } from './GreetBand';
import { Insights } from './Insights';
import { Kpis } from './Kpis';
import { NewOrders } from './NewOrders';
import { useOverview } from './queries';
import { SalesByHour } from './SalesByHour';
import { SetupCard } from './SetupCard';
import { TopItems } from './TopItems';
import { Topbar } from './Topbar';
import styles from './overview.module.css';
import type { OverviewData, Period } from './types';
import { useNarrow } from '../shell/useNarrow';

/** B-03 Overview, the business landing page (`/`). */
export function OverviewPage() {
  return (
    <ToastProvider>
      <OverviewContent />
    </ToastProvider>
  );
}

function OverviewContent() {
  const intl = useIntl();
  const [period, setPeriod] = useState<Period>('today');
  const query = useOverview(period);

  if (query.isPending) {
    return (
      <div className={styles.page} aria-busy="true">
        <VisuallyHidden>{intl.formatMessage({ id: 'overview.loading' })}</VisuallyHidden>
        <Skeleton shape="block" height="120px" />
        <Skeleton shape="block" height="140px" />
        <Skeleton shape="block" height="320px" />
      </div>
    );
  }
  if (query.isError) {
    return (
      <div className={styles.page}>
        <Banner
          tone="warning"
          title={intl.formatMessage({ id: 'overview.error' })}
          action={
            <Button size="sm" onClick={() => void query.refetch()}>
              {intl.formatMessage({ id: 'overview.retry' })}
            </Button>
          }
        />
      </div>
    );
  }
  return <Dashboard data={query.data} period={period} onPeriod={setPeriod} />;
}

interface DashboardProps {
  data: OverviewData;
  period: Period;
  onPeriod: (p: Period) => void;
}

function Dashboard({ data, period, onPeriod }: DashboardProps) {
  const intl = useIntl();
  const toast = useToast();
  // The sidebar has the setup card from 1024px; below that it sits under the KPIs (Shell.tsx).
  const narrow = useNarrow();
  // "Not useful" hides the row for this visit; the next row of another kind moves up (D-99).
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const onNotUseful = useCallback(
    (row: InsightRow) => {
      setHidden((h) => new Set(h).add(row.id));
      toast.show({ message: intl.formatMessage({ id: 'overview.insights.hiddenToast' }) });
    },
    [intl, toast],
  );
  return (
    <div className={styles.page}>
      <Topbar syncedAt={data.syncedAt} storeName={data.storeName} />
      <GreetBand data={data} period={period} onPeriod={onPeriod} />
      <div className={styles.grid}>
        <div className={styles.areaKpi}>
          <Kpis data={data} />
        </div>
        {narrow && (
          <div className={styles.areaSetup}>
            <SetupCard setup={data.setup} />
          </div>
        )}
        <div className={styles.areaChart}>
          <SalesByHour data={data} />
        </div>
        <div className={styles.areaOrders}>
          <NewOrders orders={data.newOrders} />
        </div>
        <div className={styles.areaInsights}>
          <Insights insights={data.insights.filter((i) => !hidden.has(i.id))} onNotUseful={onNotUseful} />
        </div>
        <div className={styles.areaTop}>
          <TopItems data={data} />
        </div>
      </div>
      <CoachSlot />
    </div>
  );
}
