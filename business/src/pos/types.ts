// Shapes the POS code uses. They follow 04-api.md "POS and sync", 03-data-model.md (`pos_sales`,
// `pos_sale_lines`) and 02 §4.3, because W2-04's generated contract is not on this branch yet.
// TODO(merge): when `@todayz/api-client` has the pos, sync and receipts operations, derive the wire
// types in `api.ts` from the generated models; the local types here (cart, outbox, history) stay.

export type PaymentMethod = 'cash' | 'upi' | 'card';
export const PAYMENT_METHODS: readonly PaymentMethod[] = ['cash', 'upi', 'card'];

/** One item of the offline catalogue snapshot (items, prices, stock, barcodes; 02 §4.3). */
export interface CatalogueItem {
  /** The store item id (`storeItemId`). */
  id: string;
  name: string;
  variant?: string | null;
  packSize: number;
  unit: string;
  barcodes: string[];
  /** Shelf price per sale unit, in paise. */
  pricePaise: number;
  /** Price after the item-level offer, as the server priced it; absent when no offer applies. */
  offerPricePaise?: number | null;
  /** Units available to sell; may be 0 or negative (an offline sale can drive it below 0). */
  available: number;
  isListed: boolean;
  /** 1 is the best seller; the quick tiles show the top 24 (06 B-05). */
  topRank?: number | null;
}

/** Numbers the POS reads from `platform_settings` (01 §17); the server sends them with the snapshot. */
export interface PosSettings {
  /** `credit_min_order_value_paise`: the bill after discounts must reach this for credits (D-30). */
  creditMinOrderValuePaise: number;
  /** `pos_reversal_window_hours`. */
  reversalWindowHours: number;
  /** `pos_offline_link_max_hours`. */
  offlineLinkMaxHours: number;
}

export interface CatalogueSnapshot {
  items: CatalogueItem[];
  /** The `catalogue_changes` cursor this snapshot is current to. */
  cursor: string;
  settings: PosSettings;
  serverNow: string;
}

export interface CatalogueChange {
  seq: number;
  entityId: string;
  op: 'upsert' | 'delete';
  /** Present on an upsert. */
  item?: CatalogueItem;
}

export interface CatalogueChanges {
  changes: CatalogueChange[];
  cursor: string;
  settings?: PosSettings;
  serverNow: string;
}

/** Who the sale is linked to (D-07). `phone_unverified` never touches the ledger. */
export type SaleLink =
  | { method: 'bill_qr'; claimId: string; customerName?: string | null; creditsUsed: number }
  | {
      method: 'counter_code';
      phone: string;
      /** The typed 4-digit code. Kept until the server verifies it at sync (D-12), then dropped. */
      code: string;
      /** Device time when the code was typed. */
      codeAt: string;
      /** Device clock minus server clock at the last online check, in ms (D-12). */
      clockOffsetMs: number | null;
    }
  | { method: 'phone_unverified'; phone: string };

export interface SaleLine {
  /** Client ULID; the server reuses it as the movement's `client_id`. */
  clientId: string;
  /** Null for a custom line. */
  storeItemId: string | null;
  name: string;
  unitPricePaise: number;
  quantity: number;
  /** Manual line discount in paise (manager and above). */
  discountPaise: number;
  lineTotalPaise: number;
}

/** A finished counter sale, as recorded on this device. */
export interface Sale {
  clientId: string;
  deviceId: string;
  deviceSeq: number;
  /** Device time of the sale (ISO). */
  soldAt: string;
  lines: SaleLine[];
  /** Σ line totals after line discounts and item offers, before the bill discount. */
  subtotalPaise: number;
  billDiscountPaise: number;
  roundOffPaise: number;
  /** Credits taken off the bill by a bill QR claim. */
  creditsUsed: number;
  /** What the customer pays: subtotal − discount + round-off − credits. */
  totalPaise: number;
  paymentMethod: PaymentMethod;
  link: SaleLink | null;
}

export type SaleState =
  /** In the outbox, not yet acknowledged by the server. */
  | 'pending'
  /** Sent, an answer is awaited (or the attempt failed and will be retried). */
  | 'sending'
  | 'synced'
  /** The server refused it; it will not be retried until a person acts. */
  | 'rejected';

export type LinkStatus = 'linked' | 'pending_verification' | 'failed';

/** A sale in the local history, with what the server said about it. */
export interface SaleRecord {
  sale: Sale;
  state: SaleState;
  attempts: number;
  lastError?: string | null;
  /** Server id, once synced. */
  saleId?: string | null;
  receiptToken?: string | null;
  linkStatus?: LinkStatus | null;
  /** Items that went below 0 on the server because of this sale ("count needed"). */
  stockFlags: string[];
  /** Totals the server finished with, when they differ from what the device recorded. */
  serverTotalPaise?: number | null;
  reversed: boolean;
  cashbackCredits?: number | null;
}
