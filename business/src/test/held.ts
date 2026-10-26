import { problem, type Call, type Handler } from './render';

/** Never answers: the request stays in flight for the rest of the test (a loading state). */
export const hold = () => new Promise<Response>(() => {});

/** The first matching call fails with a 500; every later one is held open. `calls()` counts them. */
export function failThenHold(match: (c: Call) => boolean): Handler & { calls: () => number } {
  let n = 0;
  const h = ((c: Call) => {
    if (!match(c)) return undefined;
    n += 1;
    return n === 1 ? problem(500, 'internal') : hold();
  }) as Handler & { calls: () => number };
  h.calls = () => n;
  return h;
}

/** Answers matching calls with `hold()`. */
export const holding =
  (match: (c: Call) => boolean): Handler =>
  (c) =>
    match(c) ? hold() : undefined;
