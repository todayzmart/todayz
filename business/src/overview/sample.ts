import { roundToRupee } from '@todayz/i18n';
import type { HourPoint, InsightData, Kpis, OverviewData, Period, TopItem } from './types';

/**
 * Built-in sample data for B-03 until W3-03 wires the API. Synthetic kirana names only (the ones
 * in design/source/components/BusinessOverview/preview.html); no real shop or person.
 *
 * The one switch: while this is true the screen shows the "Sample data" pill. Set it to false only
 * when `queries.ts` reads a real API.
 */
export const SAMPLE_DATA = true;

/** Thursday 22 October 2026, 6:40 pm IST: the moment the design card shows. */
export const SAMPLE_NOW = '2026-10-22T18:40:00+05:30';

/** Average bill in whole rupees, as paise: total ÷ bills, rounded half up. */
export function averageBill(totalPaise: number, bills: number): number {
  return bills === 0 ? 0 : roundToRupee(Math.round(totalPaise / bills));
}

function kpis(k: Omit<Kpis, 'avgBillPaise' | 'avgBillPrevPaise'> & { billsPrev: number }): Kpis {
  const { billsPrev, ...rest } = k;
  return {
    ...rest,
    avgBillPaise: averageBill(k.salesPaise, k.bills),
    avgBillPrevPaise: averageBill(k.salesPrevPaise, billsPrev),
  };
}

const KPIS: Record<Period, Kpis> = {
  today: kpis({
    salesPaise: 48_260_00,
    salesPrevPaise: 43_090_00,
    bills: 142,
    billsPrev: 131,
    onlineOrders: 37,
    orders: 37,
    ordersPrev: 32,
    waitingOrders: 2,
    lowItems: 9,
    sellOutSoon: 5,
  }),
  '7d': kpis({
    salesPaise: 3_12_480_00,
    salesPrevPaise: 2_95_140_00,
    bills: 921,
    billsPrev: 884,
    onlineOrders: 236,
    orders: 236,
    ordersPrev: 214,
    waitingOrders: 2,
    lowItems: 9,
    sellOutSoon: 5,
  }),
  '30d': kpis({
    salesPaise: 12_86_350_00,
    salesPrevPaise: 13_04_120_00,
    bills: 3_806,
    billsPrev: 3_871,
    onlineOrders: 951,
    orders: 951,
    ordersPrev: 902,
    waitingOrders: 2,
    lowItems: 9,
    sellOutSoon: 5,
  }),
};

/** Today 8 am to 6 pm (6 pm so far), last Thursday 8 am to 9 pm; rupees × 100. */
const TODAY = [1200, 2600, 3100, 2400, 3800, 4600, 2900, 2200, 3000, 5200, 6400];
const LAST_WEEK = [1000, 2200, 2800, 2600, 3500, 4100, 3000, 2500, 2800, 4600, 5900, 6800, 5100, 2400];

const SALES_BY_HOUR: readonly HourPoint[] = LAST_WEEK.map((prev, i) => ({
  hour: 8 + i,
  todayPaise: i < TODAY.length ? (TODAY[i] ?? 0) * 100 : null,
  prevPaise: prev * 100,
}));

/**
 * Ordered by severity as the API would (D-99, 01 §12): high (selling fast at 2 days or less,
 * going to waste), medium (selling fast, slower), low (sitting on the shelf, wanted nearby,
 * bought together). The second "selling fast" row and "bought together" show that the card
 * keeps one row per kind and at most four; "bought together" moves up when a row is dismissed.
 */
const INSIGHTS: readonly InsightData[] = [
  { id: 'selling_fast:si-amul-taaza-500', kind: 'selling_fast', item: 'Amul Taaza Milk 500 ml', sold: 64, runOutDay: '2026-10-24T10:00:00+05:30', restockQty: 28, storeItemId: 'si-amul-taaza-500' },
  { id: 'going_to_waste:b-301', kind: 'going_to_waste', count: 3, days: 5, items: ['Nandini Curd', 'Modern Bread', 'Milky Mist Paneer'], batchIds: ['b-301', 'b-302', 'b-303'] },
  { id: 'selling_fast:si-tata-salt-1kg', kind: 'selling_fast', item: 'Tata Salt 1 kg', sold: 31, runOutDay: '2026-10-27T10:00:00+05:30', restockQty: 12, storeItemId: 'si-tata-salt-1kg' },
  { id: 'sitting_on_shelf:si-kissan-jam-500', kind: 'sitting_on_shelf', item: 'Kissan Mixed Fruit Jam 500 g', inStock: 12, daysSinceSale: 21, storeItemId: 'si-kissan-jam-500', pct: 10 },
  { id: 'wanted_nearby:mp-nivea-lotion-200', kind: 'wanted_nearby', item: 'Nivea Body Lotion 200 ml', searches: 14, radiusMetres: 2000, noShopNearby: true, masterProductId: 'mp-nivea-lotion-200' },
  { id: 'bought_together:si-maggi-70:si-amul-taaza-500', kind: 'bought_together', itemA: 'Maggi 2-Minute Noodles', itemB: 'Amul Taaza Milk', sharePct: 18, storeItemIds: ['si-maggi-70', 'si-amul-taaza-500'] },
];

const TOP_ITEMS: readonly TopItem[] = [
  { id: 'si-amul-taaza-500', name: 'Amul Taaza Milk', size: '500 ml', sold: 64, last7: [48, 52, 50, 61, 55, 58, 64], revenuePaise: 1_792_00, inStock: 36, low: false, lastChange: { source: 'pos', at: '2026-10-22T18:38:00+05:30' } },
  { id: 'si-aashirvaad-atta-5kg', name: 'Aashirvaad Atta', size: '5 kg', sold: 18, last7: [22, 15, 19, 12, 20, 16, 18], revenuePaise: 5_202_00, inStock: 24, low: false, lastChange: { source: 'online', at: '2026-10-22T18:21:00+05:30' } },
  { id: 'si-fortune-sunflower-1l', name: 'Fortune Sunflower Oil', size: '1 L', sold: 14, last7: [6, 8, 7, 9, 11, 12, 14], revenuePaise: 2_310_00, inStock: 3, low: true, lastChange: { source: 'voice', at: '2026-10-22T17:02:00+05:30' } },
];

/** The whole Overview for one period, as a fresh object (callers may not share mutable state). */
export function sampleOverview(period: Period): OverviewData {
  return {
    isSample: SAMPLE_DATA,
    period,
    ownerName: 'Kavya',
    storeName: 'Sri Lakshmi Provision Stores',
    now: SAMPLE_NOW,
    syncedAt: SAMPLE_NOW,
    kpis: { ...KPIS[period] },
    newOrders: [
      { id: 'o-24821', code: 'TZ-24821', customer: 'Ananya R.', summary: 'Aashirvaad Atta 5 kg, Amul Taaza ×2, Maggi', totalPaise: 401_00, fulfilment: 'pickup', secondsToAccept: 222 },
      { id: 'o-24822', code: 'TZ-24822', customer: 'Vikram P.', summary: 'Fortune Oil 1 L, Tata Salt, Surf Excel', totalPaise: 333_00, fulfilment: 'delivery', secondsToAccept: 298 },
    ],
    salesByHour: SALES_BY_HOUR,
    insights: INSIGHTS,
    topItems: TOP_ITEMS,
    distinctItemsSold: 86,
  };
}
