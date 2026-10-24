/**
 * Stock endpoints that are in 04-api.md (W1-05) but not in the generated client yet: creating a
 * missing master product (D-37) and the bulk item import. Everything else on Stock uses the
 * generated `catalogue` client from `queries.ts`.
 *
 * TODO(merge): switch to the generated client when `createProduct`, `createItemImport` and
 * `applyItemImport` land in `backend/api/paths/catalogue.yaml`, and delete this file. The request
 * and answer shapes below are this screen's assumption of those operations.
 */
import { request } from '@todayz/api-client';
import type { Unit } from '@todayz/api-client/gen/model/unit';

export interface NewProduct {
  name: string;
  brand?: string;
  variant?: string;
  packSize: number;
  unit: Unit;
  mrpPaise?: number;
  categoryId: string;
  barcodes?: string[];
}

/** A product the shop created: it can be sold at once and waits for review by TodayZ (D-37). */
export interface CreatedProduct {
  id: string;
  name: string;
  brand: string | null;
  variant: string | null;
  packSize: number;
  unit: Unit;
  mrpPaise: number | null;
  categoryId: string;
  barcodes: string[];
  status: 'pending_review';
}

/** `POST {B}/products` (manager and above, Idempotency-Key). */
export function createProduct(bid: string, body: NewProduct, idempotencyKey: string): Promise<CreatedProduct> {
  return request<CreatedProduct>(`/v1/biz/businesses/${encodeURIComponent(bid)}/products`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    idempotencyKey,
  });
}

export type ImportErrorCode = 'required' | 'invalid' | 'too_long' | 'price_above_mrp' | 'unknown_product' | 'duplicate';

export interface ImportRowError {
  /** The column the problem is in: name, barcode, pricePaise, mrpPaise, quantity, expiresOn. */
  field: string;
  code: ImportErrorCode | (string & {});
}

export interface ImportRow {
  /** The row number in the file, counting the header as row 1. */
  row: number;
  status: 'ok' | 'error';
  name: string | null;
  barcode: string | null;
  pricePaise: number | null;
  quantity: number | null;
  errors: ImportRowError[];
}

/** The validated preview of an uploaded file; nothing is written until it is applied. */
export interface ImportPreview {
  id: string;
  fileName: string;
  rows: ImportRow[];
  counts: { ok: number; error: number };
}

export interface ImportApplied {
  applied: number;
  skipped: number;
}

/** `POST {S}/item-imports` (manager and above): multipart `file`, CSV or XLSX. */
export function uploadItemImport(bid: string, sid: string, file: File): Promise<ImportPreview> {
  const body = new FormData();
  body.set('file', file);
  return request<ImportPreview>(`/v1/biz/businesses/${encodeURIComponent(bid)}/stores/${encodeURIComponent(sid)}/item-imports`, {
    method: 'POST',
    body,
  });
}

/** `POST {S}/item-imports/{importId}/apply`: writes `import` movements for the rows without errors (Idempotency-Key). */
export function applyItemImport(bid: string, sid: string, importId: string, idempotencyKey: string): Promise<ImportApplied> {
  return request<ImportApplied>(
    `/v1/biz/businesses/${encodeURIComponent(bid)}/stores/${encodeURIComponent(sid)}/item-imports/${encodeURIComponent(importId)}/apply`,
    { method: 'POST', idempotencyKey },
  );
}
