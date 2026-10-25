import type { AiLineOutcome } from '@todayz/api-client/gen/model/aiLineOutcome';
import type { AiLineRefusal } from '@todayz/api-client/gen/model/aiLineRefusal';
import type { AiLineResult } from '@todayz/api-client/gen/model/aiLineResult';

/**
 * Where a draft stands after a partial confirm (status `ready`): the settled lines keep their
 * outcome and are never sent again; the refused ones (not_permitted, with a reason) stay open so
 * the person can fix them and confirm again.
 */
export interface Outcomes {
  settled: ReadonlyMap<string, AiLineOutcome>;
  refused: ReadonlyMap<string, AiLineRefusal | undefined>;
  /** Lines a partial confirm already added. */
  added: number;
}

export function outcomesOf(lines: readonly AiLineResult[]): Outcomes {
  const settled = new Map<string, AiLineOutcome>();
  const refused = new Map<string, AiLineRefusal | undefined>();
  for (const r of lines) {
    if (r.outcome === 'not_permitted') refused.set(r.lineId, r.reason);
    else settled.set(r.lineId, r.outcome);
  }
  const added = [...settled.values()].filter((o) => o !== 'discarded').length;
  return { settled, refused, added };
}
