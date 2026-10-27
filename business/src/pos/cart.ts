// The cart at the counter, as a reducer: every change is an action, so the screen, the keyboard
// shortcuts and the tests all go through the same code. Ids come in with the action (the reducer
// stays pure and replayable).
import { billTotals, lineTotal, toSaleLine, type BillTotals } from './money';
import { sellPricePaise } from './catalogue';
import type { CatalogueItem, PaymentMethod, SaleLine, SaleLink } from './types';

export interface CartLine {
  /** Client ULID of the line; becomes the sale line's `clientId`. */
  id: string;
  storeItemId: string | null;
  name: string;
  unitPricePaise: number;
  quantity: number;
  discountPaise: number;
  /** Units available to sell, as the catalogue last said; the line never holds more (D-270). Null for a custom line. */
  available: number | null;
}

export interface CartState {
  lines: CartLine[];
  billDiscountPaise: number;
  paymentMethod: PaymentMethod;
  link: SaleLink | null;
}

export const emptyCart: CartState = { lines: [], billDiscountPaise: 0, paymentMethod: 'cash', link: null };

export const MAX_QUANTITY = 999;

export type CartAction =
  | { type: 'add'; item: CatalogueItem; lineId: string; quantity?: number }
  | { type: 'addCustom'; lineId: string; name: string; unitPricePaise: number; quantity?: number }
  | { type: 'setQuantity'; lineId: string; quantity: number }
  | { type: 'step'; lineId: string; by: 1 | -1 }
  /** After a stock count: the item's line holds `quantity`, at most the new available; 0 removes it (D-270). */
  | { type: 'fit'; item: CatalogueItem; lineId: string; quantity: number }
  | { type: 'remove'; lineId: string }
  | { type: 'lineDiscount'; lineId: string; discountPaise: number }
  | { type: 'billDiscount'; discountPaise: number }
  | { type: 'payment'; method: PaymentMethod }
  | { type: 'link'; link: SaleLink | null }
  | { type: 'clear' };

const clampQty = (n: number) => Math.max(0, Math.min(MAX_QUANTITY, Math.floor(n)));
/** A line of a catalogue item is never raised past the item's available units (D-270). */
const capQty = (n: number, available: number | null) => (available === null ? clampQty(n) : Math.min(clampQty(n), Math.max(0, available)));
/** The quantity a line moves to: lowering always works; raising stops at available. */
const nextQty = (current: number, wanted: number, available: number | null) => {
  const w = clampQty(wanted);
  return w <= current ? w : Math.max(current, capQty(w, available));
};

function mapLine(state: CartState, id: string, fn: (l: CartLine) => CartLine | null): CartState {
  const lines = state.lines.flatMap((l) => {
    if (l.id !== id) return [l];
    const next = fn(l);
    return next ? [next] : [];
  });
  return { ...state, lines };
}

export function cartReducer(state: CartState, action: CartAction): CartState {
  switch (action.type) {
    case 'add': {
      const qty = clampQty(action.quantity ?? 1);
      if (qty === 0) return state;
      const available = action.item.available;
      const existing = state.lines.find((l) => l.storeItemId === action.item.id);
      if (existing) return mapLine(state, existing.id, (l) => ({ ...l, available, quantity: nextQty(l.quantity, l.quantity + qty, available) }));
      const capped = capQty(qty, available);
      if (capped === 0) return state;
      return { ...state, lines: [...state.lines, newLine(action.item, action.lineId, capped)] };
    }
    case 'fit': {
      const available = action.item.available;
      const qty = capQty(action.quantity, available);
      const existing = state.lines.find((l) => l.storeItemId === action.item.id);
      if (existing) return mapLine(state, existing.id, (l) => (qty === 0 ? null : { ...l, available, quantity: qty }));
      if (qty === 0) return state;
      return { ...state, lines: [...state.lines, newLine(action.item, action.lineId, qty)] };
    }
    case 'addCustom': {
      const name = action.name.trim();
      const qty = clampQty(action.quantity ?? 1);
      if (!name || qty === 0 || action.unitPricePaise < 0) return state;
      const line: CartLine = { id: action.lineId, storeItemId: null, name, unitPricePaise: action.unitPricePaise, quantity: qty, discountPaise: 0, available: null };
      return { ...state, lines: [...state.lines, line] };
    }
    case 'setQuantity':
      return mapLine(state, action.lineId, (l) => {
        const qty = nextQty(l.quantity, action.quantity, l.available);
        return qty === 0 ? null : { ...l, quantity: qty };
      });
    case 'step':
      return mapLine(state, action.lineId, (l) => {
        const qty = nextQty(l.quantity, l.quantity + action.by, l.available);
        return qty === 0 ? null : { ...l, quantity: qty };
      });
    case 'remove':
      return mapLine(state, action.lineId, () => null);
    case 'lineDiscount':
      return mapLine(state, action.lineId, (l) => ({ ...l, discountPaise: Math.max(0, Math.round(action.discountPaise)) }));
    case 'billDiscount':
      return { ...state, billDiscountPaise: Math.max(0, Math.round(action.discountPaise)) };
    case 'payment':
      return { ...state, paymentMethod: action.method };
    case 'link':
      return { ...state, link: action.link };
    case 'clear':
      return { ...emptyCart, paymentMethod: state.paymentMethod };
  }
}

export function cartTotals(state: CartState): BillTotals {
  return billTotals(state.lines, state.billDiscountPaise);
}

/**
 * The first line that holds more than its item's available units now (`availableOf` reads the live
 * catalogue: a sync or an online order may have lowered it since the line was added). The counter
 * asks for a count before it charges such a bill (D-270).
 */
export function firstShortLine(state: CartState, availableOf: (storeItemId: string) => number | null): CartLine | null {
  for (const l of state.lines) {
    if (l.storeItemId === null) continue;
    const avail = availableOf(l.storeItemId) ?? l.available;
    if (avail !== null && l.quantity > Math.max(0, avail)) return l;
  }
  return null;
}

function newLine(item: CatalogueItem, id: string, quantity: number): CartLine {
  return {
    id,
    storeItemId: item.id,
    name: item.variant ? `${item.name} ${item.variant}` : item.name,
    unitPricePaise: sellPricePaise(item),
    quantity,
    discountPaise: 0,
    available: item.available,
  };
}

export function toSaleLines(state: CartState): SaleLine[] {
  return state.lines.map((l) => toSaleLine(l.id, l));
}

export { lineTotal };
