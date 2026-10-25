import { Tabs, ToastProvider } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { StoreGate } from '../overview/StoreGate';
import { InsightsTab } from './InsightsTab';
import { SalesTab } from './SalesTab';
import type { AnalyticsSearch, AnalyticsTab } from './search';
import styles from './analytics.module.css';

/** B-12 Analytics: Sales and Insights (D-99). Owners and managers only. */
export function AnalyticsPage({ search }: { search: AnalyticsSearch }) {
  return (
    <ToastProvider>
      <StoreGate next="/analytics">{(store) => <Analytics store={store} search={search} />}</StoreGate>
    </ToastProvider>
  );
}

function Analytics({ store, search }: { store: CurrentStore; search: AnalyticsSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const tab: AnalyticsTab = search.tab ?? 'sales';
  const go = (next: AnalyticsTab) => void navigate({ to: '/analytics', search: next === 'sales' ? {} : { tab: next }, replace: true });
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'analytics.title' })}</h1>
      <p className={styles.sub}>{store.name}</p>
      <Tabs
        label={intl.formatMessage({ id: 'analytics.tabs' })}
        value={tab}
        onChange={go}
        tabs={[
          { value: 'sales', label: intl.formatMessage({ id: 'analytics.tab.sales' }), panel: tab === 'sales' ? <SalesTab store={store} /> : undefined },
          {
            value: 'insights',
            label: intl.formatMessage({ id: 'analytics.tab.insights' }),
            panel: tab === 'insights' ? <InsightsTab store={store} kind={search.kind} /> : undefined,
          },
        ]}
      />
    </div>
  );
}
