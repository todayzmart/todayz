// Shapes the POS code keeps on the device (catalogue, cart, outbox, history). The wire shapes are the
// generated models, and `api.ts` is the only file that converts between the two.

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
  /** Units available to sell: on hand less what accepted online orders hold. The counter never sells past it (D-270). */
  available: number;
  /** Units on the shelf, the online orders' units included. Absent in a snapshot stored before D-270. */
  onHand?: number;
  /** Units set aside for accepted online orders not yet collected or delivered ("blocked for online orders"). */
  reserved?: number;
  /** The server's on hand went below 0 after an offline race; it reads 0 here until someone counts (D-270). */
  countNeeded?: boolean;
  isListed: boolean;
  /** 1 is the best seller; the quick tiles show the top 24 (06 B-05). */
  topRank?: number | null;
}

/**
 * Numbers the POS would read from `platform_settings` (01 §17). No route sends them yet, so each is
 * `null` (unknown) and the POS does not gate on it: the server enforces the rule and the screen shows
 * its answer (`below_credit_minimum`, `reversal_window_passed`, a failed link).
 * TODO(lead): a settings feed for the POS, or these three numbers on the sync response.
 */
export interface PosSettings {
  /** `credit_min_order_value_paise`: the bill after discounts must reach this for credits (D-30). */
  creditMinOrderValuePaise: number | null;
  /** `pos_reversal_window_hours`. */
  reversalWindowHours: number | null;
  /** `pos_offline_link_max_hours`. */
  offlineLinkMaxHours: number | null;
}

export interface CatalogueSnapshot {
  items: CatalogueItem[];
  /** The `catalogue_changes` cursor this snapshot is current to. */
  cursor: string;
  settings: PosSettings;
  serverNow: string;
}

/** What a change feed carries about an item: price and stock, not barcodes or pack size. */
export type CatalogueItemPatch = Pick<CatalogueItem, 'id' | 'name' | 'pricePaise' | 'available' | 'isListed'> & Partial<CatalogueItem>;

export interface CatalogueChange {
  seq: number;
  entityId: string;
  op: 'upsert' | 'delete';
  /** Present on an upsert; merged into the stored item. */
  item?: CatalogueItemPatch;
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
