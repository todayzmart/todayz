import { problem, type Call, type Handler } from './render';

/** Never answers: the request stays in flight for the rest of the test (a loading state). */
export const hold = () => new Promise<Response>(() => {});

/**
 * The first `fails` matching calls fail with a 500; every later one is held open. `calls()` counts
 * them. A query with the app's own `retry: 1` needs `fails = 2` to reach its error state.
 */
export function failThenHold(match: (c: Call) => boolean, fails = 1): Handler & { calls: () => number } {
  let n = 0;
  const h = ((c: Call) => {
    if (!match(c)) return undefined;
    n += 1;
    return n <= fails ? problem(500, 'internal') : hold();
  }) as Handler & { calls: () => number };
  h.calls = () => n;
  return h;
}

/** Answers matching calls with `hold()`. */
export const holding =
  (match: (c: Call) => boolean): Handler =>
  (c) =>
    match(c) ? hold() : undefined;
