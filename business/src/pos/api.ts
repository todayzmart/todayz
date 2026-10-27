// The one place the POS talks to the server. Everything else depends on the `PosApi` interface, so
// tests pass a fake and a contract change touches this file only. `httpPosApi` is built on the
// generated operations (`createPosSale`, `syncStore`, `createBillClaim`, …) and `listStoreItems`.
//
// The contract has no catalogue snapshot route (04 lists one, the W1-05 contract does not carry it),
// so the snapshot is the store's item list plus the cursor a sync from 0 ends on. See the report.
import { newUlid } from '@todayz/auth';
import { createBillClaim, createPosSale, getBillClaim, reversePosSale, verifyCounterCode } from '@todayz/api-client/gen/pos/pos';
import { listStoreItems, getStoreItem } from '@todayz/api-client/gen/catalogue/catalogue';
import { syncStore } from '@todayz/api-client/gen/sync/sync';
import type { PosSale } from '@todayz/api-client/gen/model/posSale';
import type { PosSaleCreateRequest } from '@todayz/api-client/gen/model/posSaleCreateRequest';
import type { PosSaleLinkInput } from '@todayz/api-client/gen/model/posSaleLinkInput';
import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import type { SyncChange } from '@todayz/api-client/gen/model/syncChange';
import type { SyncEntry } from '@todayz/api-client/gen/model/syncEntry';
import type { SyncEntryResult } from '@todayz/api-client/gen/model/syncEntryResult';
import type { SyncSaleLink } from '@todayz/api-client/gen/model/syncSaleLink';
import type {
  CatalogueChange,
  CatalogueItem,
  CatalogueSnapshot,
  LinkStatus,
  PosSettings,
  Sale,
  SaleLink,
} from './types';
import type { OutboxEntry, StockMovement } from '../sync/types';

/** A sale as it goes over the wire on the online path (`POST pos/sales`). */
export type SaleWire = PosSaleCreateRequest;

export interface SaleAck {
  saleId: string;
  /** Only in the answer that rang the sale; a replay has none (the server keeps a hash). */
  receiptToken: string | null;
  /** The server's pricing, shown as returned (offers, manual discounts). */
  lines?: { clientId: string; unitPricePaise: number; discountPaise: number; lineTotalPaise: number }[];
  /** The bill-level discount: the manual one plus any bill offer. */
  billDiscountPaise?: number;
  roundOffPaise: number;
  totalPaise: number;
  creditsUsed?: number;
  cashbackCredits?: number;
  linkStatus?: LinkStatus | null;
  /** Store item ids that went below 0. Only a sync can tell; an online sale reports none. */
  stockFlags?: string[];
  serverNow: string;
}

/** What the engine sends to `api.sync`; the adapter turns it into the generated `SyncRequest`. */
export interface SyncRequest {
  deviceId: string;
  cursor: string | null;
  /** Device clock minus server clock at the last online check, in ms. */
  clockOffsetMs: number | null;
  entries: { clientId: string; seq: number; kind: 'sale' | 'movement'; at: string; payload: Sale | StockMovement }[];
}

export type SyncResult =
  | ({ clientId: string; status: 'applied' | 'duplicate' } & Partial<SaleAck>)
  | { clientId: string; status: 'rejected'; code: string };

export interface SyncResponse {
  results: SyncResult[];
  changes: CatalogueChange[];
  cursor: string;
  /** More changes wait after `cursor`. */
  hasMore?: boolean;
  settings?: PosSettings;
  serverNow: string;
}

export interface CatalogueChanges {
  changes: CatalogueChange[];
  cursor: string;
  settings?: PosSettings;
  serverNow: string;
}

export interface BillClaimDraft {
  saleClientId: string;
  amountPaise: number;
}

export interface BillClaim {
  claimId: string;
  /** The signed token the QR carries (120 s expiry). */
  token: string;
  expiresAt: string;
  serverNow: string;
}

export interface BillClaimStatus {
  claimId: string;
  status: 'open' | 'claimed' | 'expired' | 'cancelled';
  expiresAt: string;
  claimedBy?: { displayName: string } | null;
  creditsRequested?: number;
  serverNow: string;
}

export interface CounterCodeResult {
  linked: boolean;
  customerName?: string | null;
  serverNow: string;
}

export interface ReverseAck {
  /** What the confirm step lists after the fact (D-08). */
  stockReturned: { storeItemId: string; quantity: number }[];
  creditsReturned: number;
  cashbackClawedBack: number;
}

export interface PosApi {
  /** The whole catalogue and the cursor it is current to. */
  getSnapshot(): Promise<CatalogueSnapshot>;
  /** Rejects with `ApiError` code `resync_required` (410) when the cursor is too old (D-36). */
  getChanges(cursor: string): Promise<CatalogueChanges>;
  /** One item in full (barcodes, pack): a change feed only carries price and stock. */
  getItem(storeItemId: string): Promise<CatalogueItem>;
  sync(body: SyncRequest, idempotencyKey: string): Promise<SyncResponse>;
  createSale(sale: SaleWire, idempotencyKey: string): Promise<SaleAck>;
  reverseSale(saleId: string, idempotencyKey: string, reason: string): Promise<ReverseAck>;
  createBillClaim(draft: BillClaimDraft, idempotencyKey: string): Promise<BillClaim>;
  getBillClaim(claimId: string): Promise<BillClaimStatus>;
  verifyCounterCode(body: { phone: string; code: string; saleClientId: string }, idempotencyKey: string): Promise<CounterCodeResult>;
}

/** No number reaches the POS from `platform_settings` yet, so none is assumed: the server enforces them. */
export const NO_SETTINGS: PosSettings = { creditMinOrderValuePaise: null, reversalWindowHours: null, offlineLinkMaxHours: null };

/** Ten digits as typed at the counter, to E.164. */
export function e164(phone: string): string {
  return phone.startsWith('+') ? phone : `+91${phone}`;
}

function linkToOnline(l: SaleLink | null): PosSaleLinkInput | undefined {
  if (!l) return undefined;
  if (l.method === 'bill_qr') return { method: 'bill_qr', claimId: l.claimId };
  return { method: l.method, phone: e164(l.phone) };
}

/** The wire form of a recorded sale for `POST pos/sales`. The server prices it; no expected total is sent. */
export function saleToWire(sale: Sale): SaleWire {
  const link = linkToOnline(sale.link);
  return {
    clientId: sale.clientId,
    lines: sale.lines.map((l) => ({
      clientId: l.clientId,
      ...(l.storeItemId ? { storeItemId: l.storeItemId } : {}),
      name: l.name,
      unitPricePaise: l.unitPricePaise,
      quantity: l.quantity,
      ...(l.discountPaise > 0 ? { manualDiscountPaise: l.discountPaise } : {}),
    })),
    ...(sale.billDiscountPaise > 0 ? { billDiscountPaise: sale.billDiscountPaise } : {}),
    paymentMethod: sale.paymentMethod,
    ...(link ? { link } : {}),
  };
}

/** Whether the sale needs the server in the same breath (a bill QR claim cannot sync later). */
export function needsOnline(sale: Sale): boolean {
  return sale.link?.method === 'bill_qr';
}

function linkToSync(l: SaleLink | null): SyncSaleLink | undefined {
  if (!l || l.method === 'bill_qr') return undefined;
  if (l.method === 'phone_unverified' || !l.code) return { method: 'phone_unverified', phone: e164(l.phone) };
  return {
    method: 'counter_code',
    phone: e164(l.phone),
    code: l.code,
    codeEnteredAt: l.codeAt,
    ...(typeof l.clockOffsetMs === 'number' ? { clockOffsetMs: Math.round(l.clockOffsetMs) } : {}),
  };
}

/** An outbox entry in the generated sync shape. */
export function toSyncEntry(e: SyncRequest['entries'][number]): SyncEntry {
  if (e.kind === 'sale') {
    const sale = e.payload as Sale;
    const link = linkToSync(sale.link);
    return {
      clientId: e.clientId,
      deviceSeq: e.seq,
      kind: 'sale',
      at: sale.soldAt,
      sale: {
        lines: sale.lines.map((l) => ({
          clientId: l.clientId,
          ...(l.storeItemId ? { storeItemId: l.storeItemId } : {}),
          name: l.name,
          unitPricePaise: l.unitPricePaise,
          quantity: l.quantity,
          ...(l.discountPaise > 0 ? { manualDiscountPaise: l.discountPaise } : {}),
        })),
        ...(sale.billDiscountPaise > 0 ? { billDiscountPaise: sale.billDiscountPaise } : {}),
        paymentMethod: sale.paymentMethod,
        ...(link ? { link } : {}),
      },
    };
  }
  const m = e.payload as StockMovement;
  const base = { clientId: e.clientId, deviceSeq: e.seq, at: e.at };
  if (m.kind === 'count_correction') return { ...base, kind: 'count', stock: { storeItemId: m.storeItemId, counted: m.quantity, ...(m.note ? { note: m.note } : {}) } };
  return { ...base, kind: m.kind, stock: { storeItemId: m.storeItemId, quantity: m.quantity, ...(m.note ? { note: m.note } : {}) } };
}

/** The outbox entry as the engine hands it to the adapter. */
export function entryToWire(e: OutboxEntry): SyncRequest['entries'][number] {
  return { clientId: e.clientId, seq: e.seq, kind: e.kind, at: e.createdAt, payload: e.payload };
}

export function toCatalogueItem(s: StoreItem): CatalogueItem {
  return {
    id: s.id,
    name: s.name,
    variant: s.variant,
    packSize: s.packSize,
    unit: s.unit,
    barcodes: s.barcodes,
    pricePaise: s.pricePaise,
    available: s.available,
    isListed: s.isListed,
  };
}

/** Store-item changes only: a product, category or offer change does not change what the counter sells. */
export function toCatalogueChanges(changes: readonly SyncChange[]): CatalogueChange[] {
  const out: CatalogueChange[] = [];
  for (const c of changes) {
    if (c.entity !== 'store_item') continue;
    if (c.op === 'delete') out.push({ seq: c.seq, entityId: c.entityId, op: 'delete' });
    else if (c.item) {
      const { id, name, pricePaise, available, isListed } = c.item;
      out.push({ seq: c.seq, entityId: c.entityId, op: 'upsert', item: { id, name, pricePaise, available, isListed } });
    }
  }
  return out;
}

export function toAck(s: PosSale): SaleAck {
  const manual = s.lines.reduce((n, l) => n + l.manualDiscountPaise, 0);
  return {
    saleId: s.id,
    receiptToken: s.receiptToken,
    lines: s.lines.map((l) => ({
      clientId: l.clientId,
      unitPricePaise: l.unitPricePaise,
      discountPaise: l.offerDiscountPaise + l.manualDiscountPaise,
      lineTotalPaise: l.lineTotalPaise,
    })),
    billDiscountPaise: Math.max(0, s.discountPaise - manual) + s.billOfferDiscountPaise,
    roundOffPaise: s.roundOffPaise,
    totalPaise: s.totalPaise,
    creditsUsed: s.creditsUsed,
    cashbackCredits: s.cashbackCredits,
    linkStatus: s.link?.status ?? null,
    stockFlags: [],
    serverNow: s.serverNow,
  };
}

export function toReverseAck(s: PosSale): ReverseAck {
  return {
    stockReturned: s.lines.flatMap((l) => (l.storeItemId ? [{ storeItemId: l.storeItemId, quantity: l.quantity }] : [])),
    creditsReturned: s.creditsUsed,
    cashbackClawedBack: s.cashbackCredits,
  };
}

function toSyncResult(r: SyncEntryResult, flagged: ReadonlySet<string>, sale: Sale | undefined): SyncResult {
  if (r.status === 'rejected') return { clientId: r.clientId, status: 'rejected', code: r.reason ?? 'rejected' };
  const flags = sale ? [...new Set(sale.lines.flatMap((l) => (l.storeItemId && flagged.has(l.storeItemId) ? [l.storeItemId] : [])))] : [];
  return {
    clientId: r.clientId,
    status: r.status,
    ...(r.saleId ? { saleId: r.saleId } : {}),
    receiptToken: r.receiptToken,
    linkStatus: r.linkStatus,
    stockFlags: flags,
  };
}

/** Changes page by page until `hasMore` is false (500 a page); the last answer gives the cursor. */
async function pullChanges(bid: string, sid: string, from: number) {
  const changes: SyncChange[] = [];
  let cursor = from;
  let serverNow = '';
  for (let guard = 0; guard < 200; guard += 1) {
    const r = await syncStore(bid, sid, { cursor, entries: [] }, { idempotencyKey: newUlid() });
    changes.push(...r.changes);
    cursor = r.cursor;
    serverNow = r.serverNow;
    if (!r.hasMore) break;
  }
  return { changes, cursor, serverNow };
}

/** The HTTP implementation for one store. */
export function httpPosApi(bid: string, sid: string): PosApi {
  return {
    async getSnapshot() {
      // The cursor first, then the items: whatever changes between the two is replayed by the next sync.
      const head = await pullChanges(bid, sid, 0);
      const items: CatalogueItem[] = [];
      let cursor: string | undefined;
      for (let guard = 0; guard < 500; guard += 1) {
        const page = await listStoreItems(bid, sid, { limit: 100, ...(cursor ? { cursor } : {}) });
        items.push(...page.items.map(toCatalogueItem));
        if (!page.nextCursor) break;
        cursor = page.nextCursor;
      }
      return { items, cursor: String(head.cursor), settings: NO_SETTINGS, serverNow: head.serverNow };
    },
    async getChanges(cursor) {
      const from = Number(cursor);
      const r = await pullChanges(bid, sid, Number.isFinite(from) ? from : 0);
      return { changes: toCatalogueChanges(r.changes), cursor: String(r.cursor), serverNow: r.serverNow };
    },
    async getItem(storeItemId) {
      return toCatalogueItem(await getStoreItem(bid, sid, storeItemId));
    },
    async sync(body, key) {
      const from = body.cursor === null ? 0 : Number(body.cursor);
      const r = await syncStore(bid, sid, { cursor: Number.isFinite(from) ? from : 0, entries: body.entries.map(toSyncEntry) }, { idempotencyKey: key });
      const flagged = new Set(r.changes.flatMap((c) => (c.entity === 'store_item' && c.item?.countNeeded ? [c.entityId] : [])));
      const sales = new Map(body.entries.flatMap((e) => (e.kind === 'sale' ? [[e.clientId, e.payload as Sale] as const] : [])));
      return {
        results: r.results.map((x) => toSyncResult(x, flagged, sales.get(x.clientId))),
        changes: toCatalogueChanges(r.changes),
        cursor: String(r.cursor),
        hasMore: r.hasMore,
        serverNow: r.serverNow,
      };
    },
    async createSale(sale, key) {
      return toAck(await createPosSale(bid, sid, sale, { idempotencyKey: key }));
    },
    async reverseSale(saleId, key, reason) {
      return toReverseAck(await reversePosSale(bid, sid, saleId, { reason }, { idempotencyKey: key }));
    },
    async createBillClaim(draft, key) {
      const c = await createBillClaim(bid, sid, draft, { idempotencyKey: key });
      if (!c.token) throw new Error('bill claim has no token');
      return { claimId: c.id, token: c.token, expiresAt: c.expiresAt, serverNow: c.serverNow };
    },
    async getBillClaim(claimId) {
      const c = await getBillClaim(bid, sid, claimId);
      return {
        claimId: c.id,
        status: c.status === 'used' ? 'claimed' : c.status,
        expiresAt: c.expiresAt,
        claimedBy: c.customerName ? { displayName: c.customerName } : null,
        creditsRequested: c.creditsUsed,
        serverNow: c.serverNow,
      };
    },
    async verifyCounterCode(body, key) {
      const r = await verifyCounterCode(bid, sid, { phone: e164(body.phone), code: body.code, saleClientId: body.saleClientId }, { idempotencyKey: key });
      return { linked: true, customerName: r.customerName, serverNow: r.serverNow };
    },
  };
}
