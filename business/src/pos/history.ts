// The sales history is the outbox read back: every sale on this device with what the server said.
import type { OutboxEntry } from '../sync/types';
import type { Sale, SaleRecord } from './types';

export function entryToRecord(e: OutboxEntry): SaleRecord | null {
  if (e.kind !== 'sale') return null;
  const r = e.result;
  return {
    sale: e.payload as Sale,
    state: e.state,
    attempts: e.attempts,
    lastError: e.lastError ?? r?.errorCode ?? null,
    saleId: r?.saleId ?? null,
    receiptToken: r?.receiptToken ?? null,
    linkStatus: r?.linkStatus ?? null,
    stockFlags: r?.stockFlags ?? [],
    serverTotalPaise: r?.serverTotalPaise ?? null,
    reversed: r?.reversed ?? false,
    cashbackCredits: r?.cashbackCredits ?? null,
  };
}

/** Newest first. */
export function toRecords(entries: readonly OutboxEntry[]): SaleRecord[] {
  return entries
    .flatMap((e) => {
      const r = entryToRecord(e);
      return r ? [r] : [];
    })
    .sort((a, b) => b.sale.deviceSeq - a.sale.deviceSeq);
}

/** True while a manager can still reverse the sale (`pos_reversal_window_hours`, measured from the device sale time). */
export function canReverse(rec: SaleRecord, windowHours: number, nowMs: number): boolean {
  if (rec.state !== 'synced' || rec.reversed || !rec.saleId) return false;
  const soldAt = Date.parse(rec.sale.soldAt);
  return Number.isFinite(soldAt) && nowMs - soldAt <= windowHours * 3_600_000;
}

/** Records that need a person's attention: refused, link failed, stock to count, price differs. */
export function needsAttention(rec: SaleRecord): boolean {
  return (
    rec.state === 'rejected' ||
    rec.linkStatus === 'failed' ||
    rec.stockFlags.length > 0 ||
    (rec.serverTotalPaise !== null && rec.serverTotalPaise !== undefined)
  );
}
