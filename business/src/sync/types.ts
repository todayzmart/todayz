// The outbox: every local write gets a client ULID and a device sequence number (business/CLAUDE.md,
// 02 §4.3). The sync engine sends entries in `seq` order; the server applies each one once, by
// `(store_id, client_id)`.
import type { PosSettings, Sale } from '../pos/types';

export type EntryKind = 'sale' | 'movement';

/** A stock movement made while offline (count correction, restock). W2-12 enqueues these. */
export interface StockMovement {
  storeItemId: string;
  kind: 'count_correction' | 'restock' | 'damage' | 'expired';
  quantity: number;
  note?: string;
}

export type EntryState = 'pending' | 'sending' | 'synced' | 'rejected';

/** What the server said about one applied entry. */
export interface EntryResult {
  saleId?: string | null;
  receiptToken?: string | null;
  linkStatus?: 'linked' | 'pending_verification' | 'failed' | null;
  /** Store item ids whose stock went below 0 because of this entry ("count needed"). */
  stockFlags?: string[];
  /** The total the server ended with, when it differs from the device's. */
  serverTotalPaise?: number | null;
  cashbackCredits?: number | null;
  reversed?: boolean;
  /** A problem code when the server refused the entry. */
  errorCode?: string | null;
}

export interface OutboxEntry {
  clientId: string;
  seq: number;
  kind: EntryKind;
  createdAt: string;
  state: EntryState;
  attempts: number;
  lastError: string | null;
  payload: Sale | StockMovement;
  result?: EntryResult | undefined;
}

export interface CatalogueMeta {
  cursor: string;
  settings: PosSettings;
  /** When the snapshot or the last delta was applied (device ISO time). */
  syncedAt: string;
}

/** The batch that was sent and not answered; a retry sends exactly this under the same key. */
export interface Inflight {
  key: string;
  clientIds: string[];
}

/** Device clock minus server clock, measured at the last online check (D-12). */
export interface ClockCheck {
  offsetMs: number;
  at: string;
}

export const SYNC_BATCH_MAX = 50;
