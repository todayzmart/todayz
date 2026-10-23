import { useEffect, useState } from 'react';

/**
 * Time left until `deadline`, in ms, never below 0; null when there is no deadline.
 *
 * Every countdown is computed from the `serverNow` of the response it came with (06 G9, C-12): the
 * server's clock at that moment plus the time elapsed on this page since (a monotonic clock), never
 * the device's wall clock. A new response brings a new `serverNow` and re-bases the countdown.
 */
export function useServerCountdown(deadline: string | null | undefined, serverNow: string | undefined): number | null {
  const server = serverNow ? Date.parse(serverNow) : Number.NaN;
  const end = deadline ? Date.parse(deadline) : Number.NaN;
  const active = Number.isFinite(end) && Number.isFinite(server);
  // Time elapsed on this page since `serverNow` arrived, as measured by the last tick of that response.
  const [tick, setTick] = useState<{ for: string | undefined; elapsed: number }>({ for: undefined, elapsed: 0 });

  useEffect(() => {
    if (!active) return undefined;
    const at = performance.now();
    const id = setInterval(() => setTick({ for: serverNow, elapsed: performance.now() - at }), 1000);
    return () => clearInterval(id);
  }, [active, serverNow]);

  if (!active) return null;
  const elapsed = tick.for === serverNow ? tick.elapsed : 0;
  return Math.max(0, end - (server + elapsed));
}

/** 8:42 for 522 000 ms. Rounds up so the display reaches 0:00 only when the time is out. */
export function formatMmSs(ms: number): string {
  const total = Math.ceil(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}
