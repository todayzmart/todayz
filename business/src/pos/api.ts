// The one place the POS talks to the server. Everything else depends on the `PosApi` interface, so
// tests pass a fake and the merge with W2-04 touches this file only.
// TODO(merge): switch to the generated `@todayz/api-client` operations (`pos`, `sync`, `receipts`);
// the shapes below are read from 04-api.md "POS and sync" and 03 `pos_sales`, and are assumptions
// until W2-04's contract lands (listed in docs/qa/W2-11/REPORT.md).
import { request } from '@todayz/api-client';
import type { CatalogueChanges, CatalogueSnapshot, LinkStatus, PaymentMethod, Sale, SaleLink } from './types';
import type { OutboxEntry } from '../sync/types';

const seg = encodeURIComponent;

/** A sale as it goes over the wire (POST pos/sales and a `sale` entry of POST sync). */
export interface SaleWire {
  clientId: string;
  deviceId: string;
  deviceSeq: number;
  soldAt: string;
  lines: { clientId: string; storeItemId: string | null; name: string; unitPricePaise: number; quantity: number; discountPaise: number }[];
  billDiscountPaise: number;
  /** What the device computed; the server prices again and answers with its own. */
  roundOffPaise: number;
  totalPaise: number;
  paymentMethod: PaymentMethod;
  link: SaleLinkWire | null;
}

export type SaleLinkWire =
  | { method: 'bill_qr'; claimId: string }
  | { method: 'counter_code'; phone: string; code?: string; codeAt?: string; clockOffsetMs?: number | null; verified?: boolean }
  | { method: 'phone_unverified'; phone: string };

export interface SaleAck {
  saleId: string;
  receiptToken: string;
  /** The server's pricing, shown as returned. */
  lines?: { clientId: string; unitPricePaise: number; discountPaise: number; lineTotalPaise: number }[];
  discountPaise?: number;
  roundOffPaise: number;
  totalPaise: number;
  creditsUsed?: number;
  cashbackCredits?: number;
  linkStatus?: LinkStatus | null;
  /** Store item ids that went below 0. */
  stockFlags?: string[];
  serverNow: string;
}

export interface SyncRequest {
  deviceId: string;
  cursor: string | null;
  /** Device clock minus server clock at the last online check, in ms. */
  clockOffsetMs: number | null;
  entries: { clientId: string; seq: number; kind: 'sale' | 'movement'; payload: unknown }[];
}

export type SyncResult =
  | ({ clientId: string; status: 'applied' | 'duplicate' } & Partial<SaleAck>)
  | { clientId: string; status: 'rejected'; code: string };

export interface SyncResponse {
  results: SyncResult[];
  changes: CatalogueChanges['changes'];
  cursor: string;
  settings?: CatalogueChanges['settings'];
  serverNow: string;
}

export interface BillClaimDraft {
  saleClientId: string;
  amountPaise: number;
}

export interface BillClaim {
  claimId: string;
  /** The signed token the QR carries (bill id, store, amount, 120 s expiry). */
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
  getSnapshot(): Promise<CatalogueSnapshot>;
  /** Rejects with `ApiError` code `resync_required` (410) when the cursor is older than 30 days (D-36). */
  getChanges(cursor: string): Promise<CatalogueChanges>;
  sync(body: SyncRequest, idempotencyKey: string): Promise<SyncResponse>;
  createSale(sale: SaleWire, idempotencyKey: string): Promise<SaleAck>;
  reverseSale(saleId: string, idempotencyKey: string): Promise<ReverseAck>;
  createBillClaim(draft: BillClaimDraft, idempotencyKey: string): Promise<BillClaim>;
  getBillClaim(claimId: string): Promise<BillClaimStatus>;
  verifyCounterCode(body: { phone: string; code: string; saleClientId: string }, idempotencyKey: string): Promise<CounterCodeResult>;
}

/** The wire form of a recorded sale. */
export function saleToWire(sale: Sale): SaleWire {
  return {
    clientId: sale.clientId,
    deviceId: sale.deviceId,
    deviceSeq: sale.deviceSeq,
    soldAt: sale.soldAt,
    lines: sale.lines.map((l) => ({
      clientId: l.clientId,
      storeItemId: l.storeItemId,
      name: l.name,
      unitPricePaise: l.unitPricePaise,
      quantity: l.quantity,
      discountPaise: l.discountPaise,
    })),
    billDiscountPaise: sale.billDiscountPaise,
    roundOffPaise: sale.roundOffPaise,
    totalPaise: sale.totalPaise,
    paymentMethod: sale.paymentMethod,
    link: linkToWire(sale.link),
  };
}

function linkToWire(l: SaleLink | null): SaleLinkWire | null {
  if (!l) return null;
  if (l.method === 'bill_qr') return { method: 'bill_qr', claimId: l.claimId };
  if (l.method === 'phone_unverified') return { method: 'phone_unverified', phone: l.phone };
  // A code the server already checked online carries no code; an offline one carries code and time (D-12).
  if (l.code === '') return { method: 'counter_code', phone: l.phone, verified: true };
  return { method: 'counter_code', phone: l.phone, code: l.code, codeAt: l.codeAt, clockOffsetMs: l.clockOffsetMs };
}

export function entryToWire(e: OutboxEntry): SyncRequest['entries'][number] {
  return {
    clientId: e.clientId,
    seq: e.seq,
    kind: e.kind,
    payload: e.kind === 'sale' ? saleToWire(e.payload as Sale) : e.payload,
  };
}

/** The HTTP implementation for one store (`{S}` = /v1/biz/businesses/{bid}/stores/{sid}). */
export function httpPosApi(bid: string, sid: string): PosApi {
  const S = `/v1/biz/businesses/${seg(bid)}/stores/${seg(sid)}`;
  const json = (body: unknown): RequestInit => ({
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return {
    getSnapshot: () => request<CatalogueSnapshot>(`${S}/catalogue/snapshot`),
    getChanges: (cursor) => request<CatalogueChanges>(`${S}/catalogue/changes?cursor=${seg(cursor)}`),
    sync: (body, key) => request<SyncResponse>(`${S}/sync`, { ...json(body), idempotencyKey: key }),
    createSale: (sale, key) => request<SaleAck>(`${S}/pos/sales`, { ...json(sale), idempotencyKey: key }),
    reverseSale: (saleId, key) => request<ReverseAck>(`${S}/pos/sales/${seg(saleId)}/reverse`, { ...json({}), idempotencyKey: key }),
    createBillClaim: (draft, key) => request<BillClaim>(`${S}/bill-claims`, { ...json(draft), idempotencyKey: key }),
    getBillClaim: (claimId) => request<BillClaimStatus>(`${S}/bill-claims/${seg(claimId)}`),
    verifyCounterCode: (body, key) => request<CounterCodeResult>(`${S}/counter-code/verify`, { ...json(body), idempotencyKey: key }),
  };
}
