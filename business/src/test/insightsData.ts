// Plain data with no imports, shared by the unit tests (`insightsApi.ts`) and the Playwright specs.

/** Fake answers for the owner screens (B-03, B-09, B-12): one business `b1` with store `s1`. */
export const STORE = '/v1/biz/businesses/b1/stores/s1';

/** 6:40 pm IST on the seed day. */
export const SERVER_NOW = '2026-01-05T13:10:00.000Z';

export function overview(over: Record<string, unknown> = {}) {
  return {
    range: 'today',
    storeName: 'Sri Lakshmi Provision Stores',
    ownerName: 'Kavya',
    kpis: {
      salesPaise: 48_260_00,
      salesPrevPaise: 42_000_00,
      bills: 142,
      onlineOrders: 9,
      orders: 11,
      ordersPrev: 8,
      waitingOrders: 2,
      avgBillPaise: 340_00,
      avgBillPrevPaise: 320_00,
      lowItems: 4,
      sellOutSoon: 3,
    },
    newOrders: [
      { id: 'o1', code: 'TZ-24821', customer: 'Ananya R.', summary: 'Aashirvaad Atta 5 kg', totalPaise: 412_00, fulfilment: 'pickup', acceptBy: '2026-01-05T13:14:00.000Z' },
      { id: 'o2', code: 'TZ-24822', customer: 'Ravi K.', summary: 'Amul Taaza 500 ml ×2', totalPaise: 58_00, fulfilment: 'pickup', acceptBy: '2026-01-05T13:15:00.000Z' },
    ],
    salesByHour: Array.from({ length: 11 }, (_, i) => ({ hour: 8 + i, todayPaise: (i + 1) * 580_00, prevPaise: (i + 1) * 500_00 })),
    topItems: [
      { storeItemId: 'i1', name: 'Amul Taaza', size: '500 ml', sold: 96, last7: [10, 12, 14, 13, 15, 16, 16], revenuePaise: 9304_00, inStock: 8, low: true, lastChange: { source: 'sale_pos', at: '2026-01-05T12:58:00.000Z' } },
      { storeItemId: 'i2', name: 'Tata Salt', size: '1 kg', sold: 51, last7: [6, 7, 8, 7, 8, 7, 8], revenuePaise: 5202_00, inStock: 40, low: false, lastChange: null },
    ],
    distinctItemsSold: 38,
    listedItems: 86,
    setup: { percent: 80, next: 'standee', steps: { kyc: true, products: true, staff: true, standee: false, openingHours: true } },
    serverNow: SERVER_NOW,
    ...over,
  };
}

const ready = { state: 'ready' } as const;

export const INSIGHTS = [
  {
    kind: 'selling_fast',
    subjectRef: 'i1',
    severity: 'high',
    evidence: ready,
    facts: { item: 'Amul Taaza', size: '500 ml', soldQty: 96, periodDays: 7, available: 8, runOutInDays: 1, runOutOn: '2026-01-06' },
    action: { type: 'restock', payload: { storeItemId: 'i1', qty: 40 } },
    text: null,
  },
  {
    kind: 'going_to_waste',
    subjectRef: 'bt1',
    severity: 'high',
    evidence: ready,
    facts: { expiringCount: 4, withinDays: 5, names: ['Bread', 'Curd', 'Paneer'], moreCount: 1 },
    action: { type: 'flash_offer', payload: { batchIds: ['bt1', 'bt2'] } },
    text: null,
  },
  {
    kind: 'sitting_on_shelf',
    subjectRef: 'i3',
    severity: 'low',
    evidence: ready,
    facts: { item: 'Kissan Jam', size: '500 g', onHand: 12, daysNoSale: 21, valueAtCostPaise: 480_00 },
    action: { type: 'create_offer', payload: { storeItemId: 'i3', pct: 15 } },
    text: null,
  },
  {
    kind: 'wanted_nearby',
    subjectRef: 'p9',
    severity: 'low',
    evidence: ready,
    facts: { item: 'Oat milk', size: '1 l', searches: 23, radiusM: 2000, periodDays: 7, noShopNearby: false },
    action: { type: 'add_product', payload: { productId: 'p9' } },
    text: null,
  },
];

/** `all=1` extras: a second Selling fast row and kinds below their minimum evidence. */
export const ALL_INSIGHTS = [
  INSIGHTS[0],
  {
    ...INSIGHTS[0],
    subjectRef: 'i2',
    severity: 'medium',
    facts: { item: 'Tata Salt', size: '1 kg', soldQty: 51, periodDays: 7, available: 20, runOutInDays: 3, runOutOn: '2026-01-08' },
    action: { type: 'restock', payload: { storeItemId: 'i2', qty: 10 } },
  },
  INSIGHTS[2],
  INSIGHTS[1],
  INSIGHTS[3],
  {
    kind: 'regulars_slipping',
    subjectRef: null,
    severity: null,
    evidence: { state: 'needs_data', needs: { key: 'insights.needs.regulars_slipping', params: { minLinked: 10 } } },
    facts: null,
    action: null,
    text: null,
  },
];

export const PERIOD = { days: 7, label: 'Last 7 days' };

export function analytics(range = '7d') {
  return {
    range,
    from: '2025-12-30',
    to: '2026-01-05',
    daily: ['2025-12-30', '2025-12-31', '2026-01-01', '2026-01-02', '2026-01-03', '2026-01-04', '2026-01-05'].map((day, i) => ({ day, onlinePaise: (i + 1) * 900_00, posPaise: (i + 1) * 3_100_00, onlineBills: i + 1, posBills: (i + 1) * 12 })),
    byHour: Array.from({ length: 13 }, (_, i) => ({ hour: 8 + i, paise: (i % 5 + 1) * 1_200_00 })),
    funnel: { received: 40, accepted: 34, completed: 31, rejected: 3, notAccepted: 2, noShow: 1, cancelled: 2, p50AcceptSeconds: 95, nearbyMedianAcceptSeconds: 140 },
    topItems: [
      { storeItemId: 'i1', name: 'Amul Taaza', size: '500 ml', sold: 96, revenuePaise: 9304_00 },
      { storeItemId: 'i2', name: 'Tata Salt', size: '1 kg', sold: 51, revenuePaise: 5202_00 },
    ],
    categoryMix: [
      { category: 'Dairy', paise: 42_000_00 },
      { category: 'Staples', paise: 28_000_00 },
    ],
    repeat: { customers: 40, repeatCustomers: 14, repeatPct: 35 },
    ratings: [{ week: '2025-12-29', reviews: 6, avgStars: 4.5 }],
    serverNow: SERVER_NOW,
  };
}

export const CUSTOMERS = [
  { customerId: 'c1', name: 'Meera S.', phoneMasked: '••••••4321', visits: 9, bills: 9, spendPaise: 6_480_00, lastVisitAt: '2026-01-04T11:00:00.000Z' },
  { customerId: null, name: null, phoneMasked: '••••••7788', visits: 4, bills: 4, spendPaise: 1_920_00, lastVisitAt: '2025-12-28T06:30:00.000Z' },
];
