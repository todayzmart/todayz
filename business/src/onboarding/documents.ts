import type { DocumentContentType } from '@todayz/api-client/gen/model/documentContentType';
import type { DocumentKind } from '@todayz/api-client/gen/model/documentKind';

/** The kinds the shop is asked for, in the order they are shown. `other` is left out of the UI. */
export const DOCUMENT_KINDS: readonly DocumentKind[] = ['pan', 'gst', 'shop_photo', 'storefront', 'license'];

export const MAX_DOCUMENT_BYTES = 10 * 1024 * 1024;

const TYPES: readonly string[] = ['image/jpeg', 'image/png', 'image/webp', 'application/pdf'];

export type FileProblem = 'type' | 'size' | 'empty';

/** Checks a picked file against what the API takes (`DocumentCreate`): four types, up to 10 MB. */
export function checkFile(file: { type: string; size: number }): FileProblem | null {
  if (!TYPES.includes(file.type)) return 'type';
  if (file.size <= 0) return 'empty';
  if (file.size > MAX_DOCUMENT_BYTES) return 'size';
  return null;
}

export const contentTypeOf = (file: { type: string }): DocumentContentType => file.type as DocumentContentType;
