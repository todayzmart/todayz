import type { AdminPulseAttentionReason } from '@todayz/api-client/gen/model/adminPulseAttentionReason';
import type { AdminPulseLeakStatus } from '@todayz/api-client/gen/model/adminPulseLeakStatus';
import type { AdminPulseRange } from '@todayz/api-client/gen/model/adminPulseRange';
import type { Direction, FunnelStep, TrendMetric } from './model';

/** Message ids, spelled out so the message lint can see every one is used. */
export const RANGE_LABEL: Record<AdminPulseRange, string> = {
  today: 'pulse.range.today',
  '7d': 'pulse.range.7d',
  '30d': 'pulse.range.30d',
  '90d': 'pulse.range.90d',
};

export const VS_LABEL: Record<AdminPulseRange, string> = {
  today: 'pulse.vs.today',
  '7d': 'pulse.vs.7d',
  '30d': 'pulse.vs.30d',
  '90d': 'pulse.vs.90d',
};

export const DELTA_LABEL: Record<Direction, string> = {
  up: 'pulse.delta.up',
  down: 'pulse.delta.down',
  flat: 'pulse.delta.flat',
};

export const KPI_LABEL = {
  gmv: 'pulse.kpi.gmv',
  orders: 'pulse.kpi.orders',
  success: 'pulse.kpi.success',
  customers: 'pulse.kpi.customers',
  live: 'pulse.kpi.live',
  new: 'pulse.kpi.new',
  search: 'pulse.kpi.search',
  d7: 'pulse.kpi.d7',
} as const;

export const METRIC_LABEL: Record<TrendMetric, string> = {
  gmvPaise: 'pulse.trend.gmvPaise',
  orders: 'pulse.trend.orders',
  activeCustomers: 'pulse.trend.activeCustomers',
  searches: 'pulse.trend.searches',
};

export const FUNNEL_LABEL: Record<FunnelStep['key'], string> = {
  searches: 'pulse.funnel.searches',
  searchesWithResults: 'pulse.funnel.searchesWithResults',
  storeViews: 'pulse.funnel.storeViews',
  ordersPlaced: 'pulse.funnel.ordersPlaced',
  ordersAccepted: 'pulse.funnel.ordersAccepted',
  ordersCompleted: 'pulse.funnel.ordersCompleted',
};

export const LEAK_LABEL: Record<AdminPulseLeakStatus, string> = {
  rejected: 'orders.status.rejected',
  expired: 'orders.status.expired',
  cancelled: 'orders.status.cancelled',
  delivery_failed: 'orders.status.deliveryFailed',
};

export const RETENTION_LABEL = {
  d1Bp: 'pulse.retention.d1Bp',
  d7Bp: 'pulse.retention.d7Bp',
  d30Bp: 'pulse.retention.d30Bp',
} as const;

export const CREDITS_LABEL = {
  issued: 'pulse.credits.issued',
  spent: 'pulse.credits.spent',
  expired: 'pulse.credits.expired',
} as const;

export const ATTENTION_LABEL: Record<AdminPulseAttentionReason, string> = {
  not_accepting: 'pulse.attention.reason.not_accepting',
  strikes: 'pulse.attention.reason.strikes',
  stale_stock: 'pulse.attention.reason.stale_stock',
};
