import type { VoiceStockLine } from '@todayz/api-client/gen/model/voiceStockLine';
import { describe, expect, it } from 'vitest';
import { canConfirmKind, decisions, discardAll, initialChoices, lineState, numberOk, percent, summarise, type Choice } from './lines';
import { baseType, level } from './recorder';

function line(id: string, over: Partial<VoiceStockLine> = {}): VoiceStockLine {
  return {
    lineId: id,
    kind: 'sale',
    storeItemId: `item-${id}`,
    itemName: `Item ${id}`,
    heard: `sold 2 item ${id}`,
    quantity: 2,
    pricePaise: null,
    currentPricePaise: 5800,
    onHand: 10,
    confidence: 0.9,
    matched: true,
    disabled: null,
    ...over,
  };
}

const accepted: Choice = { outcome: 'accepted' };

describe('SC-VO-03/04 who may confirm which kind', () => {
  it('lets cashiers and above sell and count, and only managers and owners restock or change prices', () => {
    for (const role of ['owner', 'manager', 'cashier'] as const) {
      expect(canConfirmKind(role, 'sale')).toBe(true);
      expect(canConfirmKind(role, 'count')).toBe(true);
    }
    expect(canConfirmKind('cashier', 'restock')).toBe(false);
    expect(canConfirmKind('cashier', 'set_price')).toBe(false);
    for (const role of ['owner', 'manager'] as const) {
      expect(canConfirmKind(role, 'restock')).toBe(true);
      expect(canConfirmKind(role, 'set_price')).toBe(true);
    }
  });
});

describe('SC-VO-06 number ranges', () => {
  it('takes 1…9999 for a sale or restock, 0…9999 for a count (0 is out of stock) and 1…10^8 paise for a price', () => {
    expect(numberOk({ kind: 'sale', quantity: 1, pricePaise: null })).toBe(true);
    expect(numberOk({ kind: 'sale', quantity: 0, pricePaise: null })).toBe(false);
    expect(numberOk({ kind: 'restock', quantity: 10000, pricePaise: null })).toBe(false);
    expect(numberOk({ kind: 'restock', quantity: 1.5, pricePaise: null })).toBe(false);
    expect(numberOk({ kind: 'count', quantity: 0, pricePaise: null })).toBe(true);
    expect(numberOk({ kind: 'count', quantity: null, pricePaise: null })).toBe(false);
    expect(numberOk({ kind: 'set_price', quantity: null, pricePaise: 5800 })).toBe(true);
    expect(numberOk({ kind: 'set_price', quantity: null, pricePaise: 0 })).toBe(false);
    expect(numberOk({ kind: 'set_price', quantity: null, pricePaise: 100_000_001 })).toBe(false);
  });
});

describe('line states', () => {
  it('marks an unmatched line pick, a missing number fix, and a server ask_manager line manager', () => {
    expect(lineState(line('a'), accepted, 'cashier')).toBe('ready');
    expect(lineState(line('a', { storeItemId: null, itemName: null, matched: false }), accepted, 'owner')).toBe('pick');
    expect(lineState(line('a', { quantity: null }), accepted, 'owner')).toBe('fix');
    expect(lineState(line('a', { kind: 'restock', disabled: 'ask_manager' }), accepted, 'cashier')).toBe('manager');
    expect(lineState(line('a'), { outcome: 'discarded' }, 'owner')).toBe('discarded');
  });

  it("reads the caller's role for an edited line: a cashier turning a sale into a restock gets a manager line", () => {
    const edit: Choice = { outcome: 'edited', kind: 'restock', storeItemId: 'item-a', itemName: 'Item a', quantity: 5, pricePaise: null };
    expect(lineState(line('a'), edit, 'cashier')).toBe('manager');
    expect(lineState(line('a'), edit, 'manager')).toBe('ready');
  });

  it('picking an item for an unmatched line makes it ready', () => {
    const l = line('a', { storeItemId: null, itemName: null, matched: false });
    const pick: Choice = { outcome: 'edited', kind: 'sale', storeItemId: 'i9', itemName: 'Maggi', quantity: 2, pricePaise: null };
    expect(lineState(l, pick, 'cashier')).toBe('ready');
  });
});

describe('the confirm body', () => {
  const lines = [
    line('a'),
    line('b', { kind: 'restock', disabled: 'ask_manager', quantity: 10 }),
    line('c', { storeItemId: null, itemName: null, matched: false }),
    line('d', { kind: 'set_price', quantity: null, pricePaise: 6000 }),
  ];

  it('counts ready, manager, blocked and discarded lines', () => {
    const choices = { ...initialChoices(lines), d: { outcome: 'discarded' } as Choice };
    expect(summarise(lines, choices, 'cashier')).toEqual({ ready: 1, manager: 1, blocked: 1, discarded: 1 });
  });

  it('sends one decision per line; a line still needing a pick is never sent as accepted', () => {
    const choices: Record<string, Choice> = {
      ...initialChoices(lines),
      d: { outcome: 'edited', kind: 'set_price', storeItemId: 'item-d', itemName: 'Item d', quantity: null, pricePaise: 5500 },
    };
    expect(decisions(lines, choices, 'manager')).toEqual([
      { lineId: 'a', outcome: 'accepted' },
      { lineId: 'b', outcome: 'accepted' },
      { lineId: 'c', outcome: 'discarded' },
      { lineId: 'd', outcome: 'edited', value: { kind: 'set_price', storeItemId: 'item-d', pricePaise: 5500 } },
    ]);
  });

  it('sends an edited quantity line without a price', () => {
    const choices: Record<string, Choice> = {
      a: { outcome: 'edited', kind: 'count', storeItemId: 'item-a', itemName: 'Item a', quantity: 0, pricePaise: null },
    };
    expect(decisions([line('a')], choices, 'cashier')).toEqual([
      { lineId: 'a', outcome: 'edited', value: { kind: 'count', storeItemId: 'item-a', quantity: 0 } },
    ]);
  });

  it('discards every line for Discard', () => {
    expect(discardAll(lines).map((d) => d.outcome)).toEqual(['discarded', 'discarded', 'discarded', 'discarded']);
  });

  it('shows confidence as a whole percent, clamped', () => {
    expect(percent(0.923)).toBe(92);
    expect(percent(1.4)).toBe(100);
    expect(percent(-1)).toBe(0);
  });
});

describe('recorder helpers', () => {
  it('drops the codec parameters from the part type', () => {
    expect(baseType('audio/webm;codecs=opus')).toBe('audio/webm');
    expect(baseType('audio/mp4')).toBe('audio/mp4');
    expect(baseType('')).toBe('audio/webm');
  });

  it('reads silence as 0 and a loud frame as up to 1', () => {
    expect(level(new Uint8Array(0))).toBe(0);
    expect(level(new Uint8Array(64).fill(128))).toBe(0);
    expect(level(new Uint8Array(64).fill(255))).toBe(1);
    const quiet = level(new Uint8Array(64).fill(138));
    expect(quiet).toBeGreaterThan(0);
    expect(quiet).toBeLessThan(1);
  });
});
