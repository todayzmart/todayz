/** The B-02 steps in order (D-274). `payout` is optional; photos are asked for strongly but do not block going live. */
export const STEPS = ['business', 'store', 'hours', 'category', 'photos', 'payout', 'live'] as const;
export type StepId = (typeof STEPS)[number];
export const OPTIONAL: ReadonlySet<StepId> = new Set(['payout']);

export interface StepState {
  id: StepId;
  done: boolean;
  /** Can be opened now: hours need a store first. */
  reachable: boolean;
}

/** One state per step from what is done; only hours wait for something (the first store). */
export function stepStates(done: Record<StepId, boolean>): StepState[] {
  return STEPS.map((id) => ({ id, done: done[id], reachable: id !== 'hours' || done.store }));
}

/** The step to open when the person has not picked one: the first needed step still to do, else go live. */
export function firstOpen(steps: StepState[]): StepId {
  return steps.find((s) => !s.done && s.reachable && !OPTIONAL.has(s.id) && s.id !== 'photos' && s.id !== 'live')?.id ?? 'live';
}

/** The next (or previous) reachable step from `at`, or null at either end. */
export function stepFrom(steps: StepState[], at: StepId, d: -1 | 1): StepId | null {
  let i = STEPS.indexOf(at) + d;
  while (steps[i] && !steps[i]?.reachable) i += d;
  return STEPS[i] ?? null;
}
