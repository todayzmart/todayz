import { AdminProductImageSource } from '@todayz/api-client/gen/model/adminProductImageSource';
import type { AdminProductStatus } from '@todayz/api-client/gen/model/adminProductStatus';
import { text, ulid } from '../app/search';
import { PRODUCT_STATUSES } from './labels';

export const TABS = ['products', 'review', 'images', 'types', 'categories', 'synonyms'] as const;
export type CatalogueTab = (typeof TABS)[number];

/** A-08 view, kept in the URL: the tab, its filters, and the row open beside the list. */
export type SubTab = Exclude<CatalogueTab, 'products'>;

export interface CatalogueSearch {
  /** Absent is the products list. */
  tab?: SubTab;
  /** Products: name, variant, brand or a barcode. Synonyms: a prefix of the term or synonym. */
  q?: string;
  /** Absent is every status. */
  status?: AdminProductStatus;
  categoryId?: string;
  /** Images: where the waiting images came from; absent is every source. */
  source?: AdminProductImageSource;
  /** Item photos: `approved` lists the kinds showing a photo; absent is the kinds waiting for a pick. */
  photos?: 'approved';
  /** The product (products tab), pending product (review tab) or image (images tab) shown in the side panel. */
  open?: string;
}

/** The contract's bound on both `q` parameters. */
export const MAX_Q = 80;

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateCatalogueSearch(raw: Record<string, unknown>): CatalogueSearch {
  const out: CatalogueSearch = {};
  const tab = text(raw.tab);
  if (tab === 'review' || tab === 'images' || tab === 'types' || tab === 'categories' || tab === 'synonyms') out.tab = tab;
  const q = text(raw.q);
  if (q && q.length <= MAX_Q) out.q = q;
  const s = text(raw.status);
  if ((PRODUCT_STATUSES as readonly string[]).includes(s)) out.status = s as AdminProductStatus;
  if (text(raw.photos) === 'approved') out.photos = 'approved';
  const source = text(raw.source);
  if ((Object.values(AdminProductImageSource) as string[]).includes(source)) out.source = source as AdminProductImageSource;
  for (const k of ['categoryId', 'open'] as const) {
    const id = ulid(raw[k]);
    if (id) out[k] = id;
  }
  return out;
}
