// C-22 baskets (D-110): the line shape the editor works on, whether the basket is saved (GET /v1/me/baskets/{id})
// or the one unsaved basket kept on this device for a signed-out customer.
import type { Basket } from '@todayz/api-client/gen/model/basket';
import type { BasketLineInput } from '@todayz/api-client/gen/model/basketLineInput';

export interface EditLine {
  /** Stable key for React: the saved line id, or a device-made id. */
  key: string;
  productId: string | null;
  /** Free text, as typed (the server stores it normalised). */
  text: string | null;
  /** What the line shows: the product name or the text. */
  label: string;
  /** "Fine · 5 kg" for a saved product line; empty for text and device lines. */
  detail: string;
  quantity: number;
}

export const QTY_MIN = 1;
/** The stepper's range is the API's (BasketLineInput.quantity 1–99), not a platform setting. */
export const QTY_MAX = 99;
/** BasketCreate.name maxLength. */
export const NAME_MAX = 40;

let seq = 0;
export function newKey(): string {
  seq += 1;
  return `l${Date.now().toString(36)}${seq}`;
}

export function fromBasket(b: Basket, unitLabel: (size: number, unit: string) => string): EditLine[] {
  return [...b.lines]
    .sort((x, y) => x.position - y.position)
    .map((l) => {
      const pack = l.packSize !== null && l.unit ? unitLabel(l.packSize, l.unit) : '';
      return {
        key: l.lineId,
        productId: l.productId,
        text: l.text,
        label: l.productName ?? l.text ?? '',
        detail: l.variant && pack ? `${l.variant} · ${pack}` : pack,
        quantity: l.quantity,
      };
    });
}

export function toInput(lines: readonly EditLine[]): BasketLineInput[] {
  return lines.map((l) => (l.productId ? { productId: l.productId, quantity: l.quantity } : { text: l.text ?? l.label, quantity: l.quantity }));
}

/** Adds a line, or one more of it when the same product (or the same words) is already there. */
export function addLine(lines: readonly EditLine[], next: Omit<EditLine, 'key'>): EditLine[] {
  const same = lines.findIndex((l) =>
    next.productId ? l.productId === next.productId : !l.productId && (l.text ?? '').toLowerCase() === (next.text ?? '').toLowerCase(),
  );
  if (same >= 0) {
    return lines.map((l, i) => (i === same ? { ...l, quantity: Math.min(QTY_MAX, l.quantity + next.quantity) } : l));
  }
  return [...lines, { ...next, key: newKey() }];
}

/** Same lines, same quantities, same order: results are shown only for the lines they were searched for. */
export function signature(lines: readonly EditLine[]): string {
  return lines.map((l) => `${l.productId ?? `t:${l.text ?? ''}`}×${l.quantity}`).join('|');
}
