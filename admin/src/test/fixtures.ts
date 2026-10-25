import type { AdminAuditEntry } from '@todayz/api-client/gen/model/adminAuditEntry';
import type { AdminCustomer } from '@todayz/api-client/gen/model/adminCustomer';
import type { AdminOrder } from '@todayz/api-client/gen/model/adminOrder';
import type { AdminOrderSummary } from '@todayz/api-client/gen/model/adminOrderSummary';

export const NOW = '2026-10-25T06:30:00Z';
export const CUSTOMER_ID = '01JCUST0000000000000000C01';
export const ORDER_ID = '01JORDER000000000000000R01';
export const STORE_ID = '01JST0RE000000000000000S01';

export function customer(over: Partial<AdminCustomer> = {}): AdminCustomer {
  return {
    id: CUSTOMER_ID,
    name: 'Meera K',
    phoneMasked: '98•••••210',
    status: 'active',
    preferredLang: 'en',
    isAdmin: false,
    isCustomer: true,
    createdAt: '2026-09-01T04:30:00Z',
    activeStrikes: 2,
    strikes: [
      { id: '01JSTRIKE00000000000000K01', reason: 'no_show', orderId: ORDER_ID, createdAt: '2026-10-20T10:00:00Z', resetAt: null, resetReason: null },
      { id: '01JSTRIKE00000000000000K02', reason: 'no_show', orderId: null, createdAt: '2026-10-22T10:00:00Z', resetAt: null, resetReason: null },
    ],
    sessions: [
      {
        id: '01JSESS0000000000000000E01',
        app: 'customer',
        userAgent: 'Chrome on Android',
        createdAt: '2026-10-20T04:30:00Z',
        lastUsedAt: '2026-10-25T05:30:00Z',
        expiresAt: '2026-11-20T04:30:00Z',
      },
    ],
    recentOrders: [
      { id: ORDER_ID, number: 'TZ-1042', storeId: STORE_ID, storeName: 'Lakshmi Stores', status: 'accepted', totalPaise: 45_000, placedAt: '2026-10-25T05:00:00Z' },
    ],
    serverNow: NOW,
    ...over,
  };
}

export function orderSummary(over: Partial<AdminOrderSummary> = {}): AdminOrderSummary {
  return {
    id: ORDER_ID,
    number: 'TZ-1042',
    storeId: STORE_ID,
    storeName: 'Lakshmi Stores',
    customerId: CUSTOMER_ID,
    customerName: 'Meera K',
    fulfilment: 'pickup',
    status: 'accepted',
    statusReason: null,
    totalPaise: 45_000,
    placedAt: '2026-10-25T05:00:00Z',
    ...over,
  };
}

export function order(over: Partial<AdminOrder> = {}): AdminOrder {
  return {
    id: ORDER_ID,
    number: 'TZ-1042',
    storeId: STORE_ID,
    storeName: 'Lakshmi Stores',
    customerId: CUSTOMER_ID,
    customerName: 'Meera K',
    fulfilment: 'pickup',
    status: 'accepted',
    statusReason: null,
    statusNote: null,
    subtotalPaise: 45_000,
    discountPaise: 0,
    creditsUsed: 0,
    deliveryFeePaise: 0,
    totalPaise: 45_000,
    cashbackCredits: 0,
    paymentStatus: 'unpaid',
    codeAttempts: 0,
    codeLocked: false,
    acceptBy: null,
    holdUntil: '2026-10-25T08:00:00Z',
    dispatchBy: null,
    placedAt: '2026-10-25T05:00:00Z',
    version: 2,
    items: [{ id: '01JLINE0000000000000000L01', name: 'Basmati rice', variant: '1 kg', unitPricePaise: 15_000, quantityRequested: 3, quantityAccepted: 3, lineTotalPaise: 45_000 }],
    events: [
      { id: '01JEVT00000000000000000V01', fromStatus: null, toStatus: 'placed', actorKind: 'customer', actorId: null, reason: null, createdAt: '2026-10-25T05:00:00Z' },
      { id: '01JEVT00000000000000000V02', fromStatus: 'placed', toStatus: 'accepted', actorKind: 'staff', actorId: null, reason: null, createdAt: '2026-10-25T05:05:00Z' },
    ],
    notifications: [],
    creditHolds: [],
    serverNow: NOW,
    ...over,
  };
}

export function auditEntry(over: Partial<AdminAuditEntry> = {}): AdminAuditEntry {
  return {
    id: '01JAUDIT00000000000000A001',
    actorKind: 'admin',
    actorId: '01JADMIN0000000000000000AD',
    actorName: 'Asha Rao',
    action: 'customer.block',
    subjectType: 'user',
    subjectId: CUSTOMER_ID,
    before: { status: 'active' },
    after: { status: 'blocked', reason: 'fake orders' },
    createdAt: '2026-10-25T06:00:00Z',
    ...over,
  };
}

export const emptyAudit = { items: [], nextCursor: null, serverNow: NOW };
