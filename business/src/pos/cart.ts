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
  /** Units on hand when the item was added, for the short-stock hint. */
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
  | { type: 'remove'; lineId: string }
  | { type: 'lineDiscount'; lineId: string; discountPaise: number }
  | { type: 'billDiscount'; discountPaise: number }
  | { type: 'payment'; method: PaymentMethod }
  | { type: 'link'; link: SaleLink | null }
  | { type: 'clear' };

const clampQty = (n: number) => Math.max(0, Math.min(MAX_QUANTITY, Math.floor(n)));

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
      const existing = state.lines.find((l) => l.storeItemId === action.item.id);
      if (existing) return mapLine(state, existing.id, (l) => ({ ...l, quantity: clampQty(l.quantity + qty) }));
      const line: CartLine = {
        id: action.lineId,
        storeItemId: action.item.id,
        name: action.item.variant ? `${action.item.name} ${action.item.variant}` : action.item.name,
        unitPricePaise: sellPricePaise(action.item),
        quantity: qty,
        discountPaise: 0,
        available: action.item.available,
      };
      return { ...state, lines: [...state.lines, line] };
    }
    case 'addCustom': {
      const name = action.name.trim();
      const qty = clampQty(action.quantity ?? 1);
      if (!name || qty === 0 || action.unitPricePaise < 0) return state;
      const line: CartLine = { id: action.lineId, storeItemId: null, name, unitPricePaise: action.unitPricePaise, quantity: qty, discountPaise: 0, available: null };
      return { ...state, lines: [...state.lines, line] };
    }
    case 'setQuantity': {
      const qty = clampQty(action.quantity);
      return mapLine(state, action.lineId, (l) => (qty === 0 ? null : { ...l, quantity: qty }));
    }
    case 'step':
      return mapLine(state, action.lineId, (l) => {
        const qty = clampQty(l.quantity + action.by);
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

/** Lines whose quantity is more than the units on hand: the counter warns, and still sells (02 §4.3). */
export function shortLines(state: CartState): CartLine[] {
  return state.lines.filter((l) => l.available !== null && l.quantity > Math.max(0, l.available));
}

export function toSaleLines(state: CartState): SaleLine[] {
  return state.lines.map((l) => toSaleLine(l.id, l));
}

export { lineTotal };
