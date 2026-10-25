import type { AdminPulseDay } from '@todayz/api-client/gen/model/adminPulseDay';
import type { AdminPulseFunnel } from '@todayz/api-client/gen/model/adminPulseFunnel';
import type { AdminPulseMap } from '@todayz/api-client/gen/model/adminPulseMap';
import type { AdminPulseKpi } from '@todayz/api-client/gen/model/adminPulseKpi';
import type { AdminPulseRateKpi } from '@todayz/api-client/gen/model/adminPulseRateKpi';

export type Direction = 'up' | 'down' | 'flat';

/** A count's change against the previous period, as a whole percent; none when there is nothing to compare. */
export function countChange(k: AdminPulseKpi): { percent: number; direction: Direction } | undefined {
  if (k.previous === null || k.previous === 0) return undefined;
  // Round the size, then sign it, so a rise and a fall of the same size read the same.
  const change = (k.value - k.previous) / k.previous;
  const percent = Math.round(Math.abs(change) * 100);
  return { percent, direction: percent === 0 ? 'flat' : change > 0 ? 'up' : 'down' };
}

/** A rate's change in percentage points, one decimal (basis points ÷ 100). */
export function rateChange(k: AdminPulseRateKpi): { points: number; direction: Direction } | undefined {
  if (k.value === null || k.previous === null) return undefined;
  const diff = k.value - k.previous;
  const points = Math.round(Math.abs(diff) / 10) / 10;
  return { points, direction: points === 0 ? 'flat' : diff > 0 ? 'up' : 'down' };
}

/** Basis points as a percent with one decimal ("86.1%"); null reads as a dash. */
export const percent = (bp: number | null) => (bp === null ? '—' : `${(bp / 100).toFixed(1)}%`);

export type TrendMetric = 'gmvPaise' | 'orders' | 'activeCustomers' | 'searches';

/** The trend chart's geometry, in an 800 × 200 viewBox (wide, so it stays short on a desktop). Pure, so it is unit tested. */
export const TREND = { width: 800, height: 200, left: 8, right: 792, top: 8, bottom: 172 } as const;

export interface TrendBar {
  date: string;
  value: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TrendModel {
  bars: TrendBar[];
  max: number;
  /** Which bars get a date under them: all up to 10 days, then about 7 spread evenly, always the last. */
  labelled: Set<number>;
}

const r1 = (n: number) => Math.round(n * 10) / 10;

export function trendModel(days: readonly AdminPulseDay[], metric: TrendMetric): TrendModel {
  const values = days.map((d) => d[metric]);
  const max = Math.max(0, ...values);
  const plotH = TREND.bottom - TREND.top;
  const slot = days.length > 0 ? (TREND.right - TREND.left) / days.length : 0;
  const width = r1(Math.max(1, slot * 0.7));
  const bars = days.map((d, i) => {
    const height = max > 0 ? r1((d[metric] / max) * plotH) : 0;
    return { date: d.date, value: d[metric], x: r1(TREND.left + i * slot + (slot - width) / 2), y: r1(TREND.bottom - height), width, height };
  });
  const every = days.length <= 10 ? 1 : Math.ceil(days.length / 7);
  const labelled = new Set<number>();
  for (let i = days.length - 1; i >= 0; i -= every) labelled.add(i);
  return { bars, max, labelled };
}

export interface FunnelStep {
  key: keyof AdminPulseFunnel;
  count: number;
  /** Width against the first step, 0–100. */
  share: number;
  /** Of the step before, in basis points; none for the first step or when the step before is 0. */
  ofPreviousBp: number | null;
}

const FUNNEL: (keyof AdminPulseFunnel)[] = ['searches', 'searchesWithResults', 'storeViews', 'ordersPlaced', 'ordersAccepted', 'ordersCompleted'];

export function funnelSteps(f: AdminPulseFunnel): FunnelStep[] {
  const first = f.searches;
  return FUNNEL.map((key, i) => {
    const before = i > 0 ? f[FUNNEL[i - 1] as keyof AdminPulseFunnel] : 0;
    return {
      key,
      count: f[key],
      share: first > 0 ? Math.min(100, r1((f[key] / first) * 100)) : 0,
      ofPreviousBp: i > 0 && before > 0 ? Math.round((f[key] / before) * 10000) : null,
    };
  });
}

/** "19 Oct" from an IST date ("2026-10-19"), without going through the device's time zone. */
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'] as const;
export function dayLabel(date: string): string {
  const [, m, d] = date.split('-');
  return `${Number(d)} ${MONTHS[Number(m) - 1] ?? ''}`;
}

export interface CityRow {
  city: string;
  stores: number;
  live: number;
}

/** Stores per city from the map feed. The demand heat needs the map layer (open question for the lead), so it is a count here. */
export function cityRows(m: AdminPulseMap): CityRow[] {
  const by = new Map<string, CityRow>();
  for (const s of m.stores) {
    const row = by.get(s.city) ?? { city: s.city, stores: 0, live: 0 };
    row.stores += 1;
    if (s.live) row.live += 1;
    by.set(s.city, row);
  }
  return [...by.values()].sort((a, b) => b.stores - a.stores || a.city.localeCompare(b.city));
}
