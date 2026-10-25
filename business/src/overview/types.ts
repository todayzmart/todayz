import type { Insight } from '@todayz/api-client/gen/model/insight';
import type { InsightKind } from '@todayz/ui';

/**
 * What the B-03 Overview screen reads: `queries.ts` maps `GET {S}/overview` and `GET {S}/insights`
 * onto this shape, so the components never see the wire format.
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
  /** Seconds left to accept when the data was read (server clock); null when there is no deadline. */
  secondsToAccept: number | null;
}

export interface HourPoint {
  /** Hour of the IST day, 0–23. */
  hour: number;
  /** Today's sales in this hour; null for hours that have not started. */
  todayPaise: number | null;
  /** Same hour on the comparison day. */
  prevPaise: number;
}

/**
 * One Demand insights row (D-99) as `GET {S}/insights` sends it, with a stable id (kind and
 * subject). `facts` holds the numbers the sentence quotes; `insightRows.tsx` writes the sentence.
 */
export interface InsightData extends Insight {
  id: string;
}

export function insightId(i: Pick<Insight, 'kind' | 'subjectRef'>): string {
  return `${i.kind}:${i.subjectRef ?? ''}`;
}

/** Where the last stock change came from: a counter sale, an online order, or any other stock entry. */
export type StockSource = 'pos' | 'online' | 'stock';

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
  /** Source and time of the last stock change; null when the stock never moved. */
  lastChange: { source: StockSource; at: string } | null;
}

/** The setup checklist steps (B-03: KYC, 50 products, staff, QR standee, plus opening hours). */
export type SetupStep = 'kyc' | 'products' | 'staff' | 'standee' | 'openingHours';

export const SETUP_STEPS: readonly SetupStep[] = ['kyc', 'products', 'staff', 'standee', 'openingHours'];

/** Shop setup checklist progress (B-03: the setup card shows until it is complete). */
export interface SetupProgress {
  /** 0 to 100. The card hides at 100. */
  percent: number;
  /** The first unfinished step, or null when only optional steps are left. */
  next: SetupStep | null;
  /** Each step, done or not. */
  steps: Readonly<Record<SetupStep, boolean>>;
}

export interface OverviewData {
  period: Period;
  ownerName: string;
  storeName: string;
  /** "Now" for this data set; the greeting, comparison weekday and current hour come from it. */
  now: string;
  syncedAt: string;
  kpis: Kpis;
  newOrders: readonly NewOrder[];
  salesByHour: readonly HourPoint[];
  topItems: readonly TopItem[];
  /** Different items sold in the period ("3 of 86 items"). */
  distinctItemsSold: number;
  setup: SetupProgress;
}

export type { InsightKind };
