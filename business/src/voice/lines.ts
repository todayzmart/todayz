import type { VoiceStockDecision } from '@todayz/api-client/gen/model/voiceStockDecision';
import type { VoiceStockLine } from '@todayz/api-client/gen/model/voiceStockLine';
import type { VoiceStockLineKind } from '@todayz/api-client/gen/model/voiceStockLineKind';
import type { ShopRole } from '../app/shop';

/** The kinds in the order the edit sheet offers them. */
export const KINDS: readonly VoiceStockLineKind[] = ['sale', 'restock', 'count', 'set_price'];

/** A line's quantity is at most this many units (VoiceStockLineValue). */
export const MAX_QUANTITY = 9999;
/** A price is at most this many paise (VoiceStockLineValue). */
export const MAX_PRICE_PAISE = 100_000_000;
/** Below this the line is marked "Check this" (the server caps an unheard number at 0.5). */
export const LOW_CONFIDENCE = 0.6;

/**
 * Who may confirm which kind (D-06, D-65): cashiers and above sell (`pos.sell`) and count
 * (`stock.count`); managers and above restock (`stock.restock`) and change prices
 * (`item.price.edit`). The server is the gate and records a line the caller may not confirm as
 * `not_permitted`; this only says so before Confirm.
 */
export function canConfirmKind(role: ShopRole, kind: VoiceStockLineKind): boolean {
  if (kind === 'sale' || kind === 'count') return true;
  return role === 'owner' || role === 'manager';
}

/** What the person has done with one line so far. */
export type Choice =
  | { outcome: 'accepted' }
  | { outcome: 'discarded' }
  | {
      outcome: 'edited';
      kind: VoiceStockLineKind;
      storeItemId: string;
      itemName: string;
      quantity: number | null;
      pricePaise: number | null;
    };

/** A line as it would be applied: the draft's values, or the person's edit. */
export interface Effective {
  kind: VoiceStockLineKind;
  storeItemId: string | null;
  itemName: string | null;
  quantity: number | null;
  pricePaise: number | null;
}

/**
 * - ready: Confirm applies it
 * - pick: no item matched; pick one or discard the line
 * - fix: the number is missing or out of range; edit or discard the line
 * - manager: the caller may not confirm this kind; it is recorded, not applied
 * - discarded: the person dropped it
 */
export type LineState = 'ready' | 'pick' | 'fix' | 'manager' | 'discarded';

export function effective(line: VoiceStockLine, choice: Choice): Effective {
  if (choice.outcome === 'edited') {
    return { kind: choice.kind, storeItemId: choice.storeItemId, itemName: choice.itemName, quantity: choice.quantity, pricePaise: choice.pricePaise };
  }
  return { kind: line.kind, storeItemId: line.storeItemId, itemName: line.itemName, quantity: line.quantity, pricePaise: line.pricePaise };
}

/** Whether the number fits the kind (the server's per-kind range rule). */
export function numberOk(e: Pick<Effective, 'kind' | 'quantity' | 'pricePaise'>): boolean {
  if (e.kind === 'set_price') return e.pricePaise !== null && e.pricePaise >= 1 && e.pricePaise <= MAX_PRICE_PAISE;
  const lo = e.kind === 'count' ? 0 : 1;
  return e.quantity !== null && Number.isInteger(e.quantity) && e.quantity >= lo && e.quantity <= MAX_QUANTITY;
}

export function lineState(line: VoiceStockLine, choice: Choice, role: ShopRole): LineState {
  if (choice.outcome === 'discarded') return 'discarded';
  const e = effective(line, choice);
  if (e.storeItemId === null) return 'pick';
  if (!numberOk(e)) return 'fix';
  // An unedited line keeps the server's own word for it (the role it read for this caller).
  if (choice.outcome === 'accepted' ? line.disabled === 'ask_manager' : !canConfirmKind(role, e.kind)) return 'manager';
  return 'ready';
}

/** Every line starts accepted; one with nothing to apply waits for the person. */
export function initialChoices(lines: readonly VoiceStockLine[]): Record<string, Choice> {
  return Object.fromEntries(lines.map((l) => [l.lineId, { outcome: 'accepted' } as Choice]));
}

export interface Summary {
  ready: number;
  manager: number;
  /** Lines that need a pick or a fix before Confirm. */
  blocked: number;
  discarded: number;
}

export function summarise(lines: readonly VoiceStockLine[], choices: Record<string, Choice>, role: ShopRole): Summary {
  const s: Summary = { ready: 0, manager: 0, blocked: 0, discarded: 0 };
  for (const l of lines) {
    const st = lineState(l, choices[l.lineId] ?? { outcome: 'accepted' }, role);
    if (st === 'ready') s.ready++;
    else if (st === 'manager') s.manager++;
    else if (st === 'discarded') s.discarded++;
    else s.blocked++;
  }
  return s;
}

/**
 * The confirm body: one decision per line. A line that still needs a pick or a fix is never sent
 * as accepted (Confirm is disabled while any is left); a manager line is sent as the person left it,
 * so the server records it `not_permitted` and applies nothing.
 */
export function decisions(lines: readonly VoiceStockLine[], choices: Record<string, Choice>, role: ShopRole): VoiceStockDecision[] {
  return lines.map((l) => {
    const c = choices[l.lineId] ?? { outcome: 'accepted' };
    const st = lineState(l, c, role);
    if (st === 'discarded' || st === 'pick' || st === 'fix') return { lineId: l.lineId, outcome: 'discarded' };
    if (c.outcome === 'edited') {
      const value =
        c.kind === 'set_price'
          ? { kind: c.kind, storeItemId: c.storeItemId, pricePaise: c.pricePaise }
          : { kind: c.kind, storeItemId: c.storeItemId, quantity: c.quantity };
      return { lineId: l.lineId, outcome: 'edited', value };
    }
    return { lineId: l.lineId, outcome: 'accepted' };
  });
}

/** Every line discarded: the draft's Discard. */
export function discardAll(lines: readonly VoiceStockLine[]): VoiceStockDecision[] {
  return lines.map((l) => ({ lineId: l.lineId, outcome: 'discarded' }));
}

/** 0.92 → 92 (a whole percent for "Sure 92%"). */
export function percent(confidence: number): number {
  return Math.round(Math.min(1, Math.max(0, confidence)) * 100);
}
