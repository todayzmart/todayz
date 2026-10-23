import { formatMoney } from '@todayz/i18n';
import { KpiStrip } from '@todayz/ui';
import { Package, ReceiptIndianRupee, ReceiptText, WalletCards } from 'lucide-react';
import { useIntl } from 'react-intl';
import { delta, weekday } from './format';
import { RouterLink } from './RouterLink';
import type { OverviewData } from './types';

/** KPI strip (B-03): sales (lead, delta vs last same weekday), orders, avg bill, items low. */
export function Kpis({ data }: { data: OverviewData }) {
  const intl = useIntl();
  const { kpis: k, period } = data;
  const compare = (style: 'long' | 'short') =>
    period === 'today'
      ? intl.formatMessage({ id: 'overview.compareShort.today' }, { weekday: weekday(intl, data.now, style) })
      : period === '7d'
        ? intl.formatMessage({ id: 'overview.compareShort.7d' })
        : intl.formatMessage({ id: 'overview.compareShort.30d' });
  return (
    <KpiStrip
      label={intl.formatMessage({ id: 'overview.kpi.label' })}
      LinkComponent={RouterLink}
      items={[
        {
          key: 'sales',
          icon: WalletCards,
          name:
            period === 'today'
              ? intl.formatMessage({ id: 'overview.kpi.sales.today' })
              : intl.formatMessage({ id: 'overview.kpi.sales.period' }),
          value: formatMoney(k.salesPaise, 'dashboard'),
          note: intl.formatMessage({ id: 'overview.kpi.sales.note' }, { bills: k.bills, online: k.onlineOrders }),
          delta: delta(intl, k.salesPaise, k.salesPrevPaise),
          comparison: compare('long'),
        },
        {
          key: 'orders',
          icon: ReceiptText,
          name: intl.formatMessage({ id: 'overview.kpi.orders' }),
          value: String(k.orders),
          note: intl.formatMessage({ id: 'overview.kpi.orders.note' }, { waiting: k.waitingOrders }),
          delta: delta(intl, k.orders, k.ordersPrev, true),
          comparison: compare('short'),
        },
        {
          key: 'avg',
          icon: ReceiptIndianRupee,
          name: intl.formatMessage({ id: 'overview.kpi.avgBill' }),
          value: formatMoney(k.avgBillPaise, 'dashboard'),
          note: intl.formatMessage({ id: 'overview.kpi.avgBill.note' }),
          delta: delta(intl, k.avgBillPaise, k.avgBillPrevPaise),
          comparison: compare('short'),
        },
        {
          key: 'low',
          icon: Package,
          name: intl.formatMessage({ id: 'overview.kpi.low' }),
          value: String(k.lowItems),
          note: intl.formatMessage({ id: 'overview.kpi.low.note' }, { soon: k.sellOutSoon }),
          link: { label: intl.formatMessage({ id: 'overview.kpi.low.link' }), href: '/stock?filter=low' },
        },
      ]}
    />
  );
}
