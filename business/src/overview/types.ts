import type { InsightKind } from '@todayz/ui';

/**
 * What the B-03 Overview screen reads. W3-03 maps the real API onto this shape and swaps the
 * query function in `queries.ts`; the components never see where the data came from.
 * Money is integer paise; times are ISO 8601 strings with an offset (read in IST).
 */

export type Period = 'today' | '7d' | '30d';

export const PERIODS: readonly Period[] = ['today', '7d', '30d'];

export interface Kpis {
  salesPaise: number;
  /** Same span one comparison period back (last same weekday for "today"). */
  salesPrevPaise: number;
  bills: number;
  onlineOrders: number;
  orders: number;
  ordersPrev: number;
  /** Online orders waiting to be accepted right now. */
  waitingOrders: number;
  avgBillPaise: number;
  avgBillPrevPaise: number;
  /** Items at or under their low-stock level. */
  lowItems: number;
  /** Of those, items that sell out by tomorrow at the current pace. */
  sellOutSoon: number;
}

export type Fulfilment = 'pickup' | 'delivery';

export interface NewOrder {
  id: string;
  /** Short code shown to the shop, e.g. TZ-24821. */
  code: string;
  /** First name and initial, e.g. "Ananya R." */
  customer: string;
  /** Short item summary, e.g. "Aashirvaad Atta 5 kg, Amul Taaza ×2, Maggi". */
  summary: string;
  totalPaise: number;
  fulfilment: Fulfilment;
  /** Seconds left to accept when the data was read. */
  secondsToAccept: number;
}

export interface HourPoint {
  /** Hour of the IST day, 0–23. */
  hour: number;
  /** Today's sales in this hour; null for hours that have not started. */
  todayPaise: number | null;
  /** Same hour on the comparison day. */
  prevPaise: number;
}

/** One Demand insights row (D-99). The sentence values are formatted by the card. */
export type InsightData =
  | { id: string; kind: 'selling_fast'; item: string; sold: number; runOutDay: string; restockQty: number; storeItemId: string }
  | { id: string; kind: 'going_to_waste'; count: number; days: number; items: readonly string[]; batchIds: readonly string[] }
  | { id: string; kind: 'sitting_on_shelf'; item: string; inStock: number; daysSinceSale: number; storeItemId: string; pct: number }
  | { id: string; kind: 'wanted_nearby'; item: string; searches: number; radiusMetres: number; noShopNearby: boolean; masterProductId: string }
  | { id: string; kind: 'bought_together'; itemA: string; itemB: string; sharePct: number; storeItemIds: readonly [string, string] };

export type StockSource = 'pos' | 'online' | 'voice';

export interface TopItem {
  id: string;
  name: string;
  /** Pack size, e.g. "500 ml". */
  size: string;
  sold: number;
  /** Units sold on each of the last 7 days, oldest first. */
  last7: readonly number[];
  revenuePaise: number;
  inStock: number;
  low: boolean;
  /** Source and time of the last stock change. */
  lastChange: { source: StockSource; at: string };
}

export interface OverviewData {
  /** True when the screen shows built-in sample data, never a real shop. Drives the "Sample data" pill. */
  isSample: boolean;
  period: Period;
  ownerName: string;
  storeName: string;
  /** "Now" for this data set; the greeting, comparison weekday and current hour come from it. */
  now: string;
  syncedAt: string;
  kpis: Kpis;
  newOrders: readonly NewOrder[];
  salesByHour: readonly HourPoint[];
  /** Ordered by severity, as the API returns them (D-99). The card keeps one per kind, at most 4. */
  insights: readonly InsightData[];
  topItems: readonly TopItem[];
  /** Different items sold in the period ("3 of 86 items"). */
  distinctItemsSold: number;
}

export type { InsightKind };
