import type { RiskFlagStatus } from '@todayz/api-client/gen/model/riskFlagStatus';
import { text, ulid } from '../app/search';

export const STATUSES = ['open', 'cleared', 'confirmed'] as const satisfies readonly RiskFlagStatus[];

/** The contract's RiskRule pattern. */
const RULE = /^[a-z][a-z0-9_.]{0,63}$/;

/** A-13 view, kept in the URL: the status, the rule tab, and the flag open beside the list. */
export interface RiskSearch {
  /** Absent is `open`: the queue comes first. */
  status?: RiskFlagStatus;
  /** Absent is every rule. */
  rule?: string;
  /** The flag shown in the side panel. */
  flag?: string;
}

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateRiskSearch(raw: Record<string, unknown>): RiskSearch {
  const out: RiskSearch = {};
  const s = text(raw.status);
  if ((STATUSES as readonly string[]).includes(s)) out.status = s as RiskFlagStatus;
  const r = text(raw.rule);
  if (RULE.test(r)) out.rule = r;
  const id = ulid(raw.flag);
  if (id) out.flag = id;
  return out;
}
