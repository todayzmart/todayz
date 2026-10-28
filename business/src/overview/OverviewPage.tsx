import { Banner, Button, Skeleton, ToastProvider, VisuallyHidden } from '@todayz/ui';
import { useState } from 'react';
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
import type { CurrentStore } from '../orders/store';
import { useNarrow } from '../shell/useNarrow';
import { StoreGate } from './StoreGate';

/** B-03 Overview, the business landing page (`/`): owners and managers. */
export function OverviewPage() {
  return (
    <ToastProvider>
      <StoreGate next="/">{(store) => <OverviewContent store={store} />}</StoreGate>
    </ToastProvider>
  );
}

function OverviewContent({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const [period, setPeriod] = useState<Period>('today');
  const query = useOverview(store, period);

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
            <Button size="sm" loading={query.isFetching} onClick={() => void query.refetch()}>
              {intl.formatMessage({ id: 'overview.retry' })}
            </Button>
          }
        />
      </div>
    );
  }
  return <Dashboard store={store} data={query.data} period={period} onPeriod={setPeriod} />;
}

interface DashboardProps {
  store: CurrentStore;
  data: OverviewData;
  period: Period;
  onPeriod: (p: Period) => void;
}

function Dashboard({ store, data, period, onPeriod }: DashboardProps) {
  // The sidebar has the setup card from 1024px; below that it sits under the KPIs (Shell.tsx).
  const narrow = useNarrow();
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
            <SetupCard setup={data.setup} storeId={store.sid} />
          </div>
        )}
        <div className={styles.areaChart}>
          <SalesByHour data={data} />
        </div>
        <div className={styles.areaOrders}>
          <NewOrders store={store} orders={data.newOrders} />
        </div>
        <div className={styles.areaInsights}>
          <Insights store={store} setup={data.setup} />
        </div>
        <div className={styles.areaTop}>
          <TopItems data={data} />
        </div>
      </div>
      <CoachSlot />
    </div>
  );
}
