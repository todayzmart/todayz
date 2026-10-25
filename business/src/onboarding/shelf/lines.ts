import type { ShelfPhotoDecision } from '@todayz/api-client/gen/model/shelfPhotoDecision';
import type { ShelfPhotoLine } from '@todayz/api-client/gen/model/shelfPhotoLine';
import { parseQuantity, parseRupees, rupeesText } from '../../stock/format';

/** What the person has on screen for one shelf product. */
export interface ShelfEdit {
  on: boolean;
  price: string;
  opening: string;
}

/** A line can be added only when it names a catalogue product the shop does not sell yet. */
export const addable = (l: ShelfPhotoLine) => l.product !== null && !l.listed;

/** The screen starts at the product's MRP, the price the server applies to an accepted line. */
export function startShelf(l: ShelfPhotoLine): ShelfEdit {
  const mrp = l.product?.mrpPaise ?? null;
  return { on: addable(l), price: mrp === null ? '' : rupeesText(mrp), opening: '' };
}

export type ShelfProblem = 'price' | 'quantity';

/**
 * The decisions to send, or the lines the browser already knows are wrong. A ticked line left at
 * the MRP with no opening stock is `accepted`; any change is `edited` with every value; an
 * unticked line is `discarded`.
 */
export function decideShelf(lines: readonly ShelfPhotoLine[], edits: Readonly<Record<string, ShelfEdit>>): { decisions: ShelfPhotoDecision[]; problems: Record<string, ShelfProblem> } {
  const problems: Record<string, ShelfProblem> = {};
  const decisions = lines.map((l): ShelfPhotoDecision => {
    const e = edits[l.lineId] ?? startShelf(l);
    if (!e.on || l.product === null || l.listed) return { lineId: l.lineId, outcome: 'discarded' };
    const pricePaise = parseRupees(e.price);
    if (pricePaise === null) problems[l.lineId] = 'price';
    const opening = e.opening.trim() === '' ? null : parseQuantity(e.opening);
    if (e.opening.trim() !== '' && opening === null) problems[l.lineId] ??= 'quantity';
    if (pricePaise === l.product.mrpPaise && opening === null) return { lineId: l.lineId, outcome: 'accepted' };
    return {
      lineId: l.lineId,
      outcome: 'edited',
      value: { productId: l.product.productId, pricePaise: pricePaise ?? 0, ...(opening === null ? {} : { openingStock: opening }) },
    };
  });
  return { decisions, problems };
}
