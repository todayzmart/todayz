import type { SupplierBillDecision } from '@todayz/api-client/gen/model/supplierBillDecision';
import type { SupplierBillLine } from '@todayz/api-client/gen/model/supplierBillLine';
import { parseQuantity, parseRupees, rupeesText } from '../stock/format';

/** What the person has on screen for one bill line. */
export interface LineEdit {
  on: boolean;
  qty: string;
  cost: string;
  expiry: string;
}

/** The screen starts from the bill: a matched line is ticked, a line with no store item is not (it cannot be added). */
export function startEdit(l: SupplierBillLine): LineEdit {
  return {
    on: l.match !== null,
    qty: String(l.quantity),
    cost: l.costPaise === null ? '' : rupeesText(l.costPaise),
    expiry: l.expiresOn ?? '',
  };
}

export type LineProblem = 'quantity' | 'cost';

/**
 * The decisions to send, or the lines the browser already knows are wrong. A ticked line the
 * person left alone is `accepted` (the server applies the bill's values); a changed one is
 * `edited` with every value; an unticked one is `discarded`.
 */
export function decide(lines: readonly SupplierBillLine[], edits: Readonly<Record<string, LineEdit>>): { decisions: SupplierBillDecision[]; problems: Record<string, LineProblem> } {
  const problems: Record<string, LineProblem> = {};
  const decisions = lines.map((l): SupplierBillDecision => {
    const e = edits[l.lineId] ?? startEdit(l);
    if (!e.on || l.match === null) return { lineId: l.lineId, outcome: 'discarded' };
    const quantity = parseQuantity(e.qty);
    if (quantity === null || quantity === 0) problems[l.lineId] = 'quantity';
    const costPaise = e.cost.trim() === '' ? null : parseRupees(e.cost);
    if (e.cost.trim() !== '' && costPaise === null) problems[l.lineId] ??= 'cost';
    const expiresOn = e.expiry === '' ? null : e.expiry;
    if (quantity === l.quantity && costPaise === l.costPaise && expiresOn === l.expiresOn) return { lineId: l.lineId, outcome: 'accepted' };
    return { lineId: l.lineId, outcome: 'edited', value: { storeItemId: l.match.storeItemId, quantity: quantity ?? 0, costPaise, expiresOn } };
  });
  return { decisions, problems };
}

/** How many lines a confirm would add to stock. */
export const addCount = (d: readonly SupplierBillDecision[]) => d.filter((x) => x.outcome !== 'discarded').length;
