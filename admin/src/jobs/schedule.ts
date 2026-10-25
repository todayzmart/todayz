/**
 * A 5-field IST cron expression as the console's English, with 12-hour times ("Every day at
 * 6:00 am", SC-OPS-06). The same shapes as the API's `scheduleText` (W1-11's schedule_text.go),
 * which writes 24-hour times; any other shape falls back to that text, then to the expression.
 * The column header says the times are IST.
 */
export function scheduleLabel(expr: string | null | undefined, fallback?: string | null): string {
  const f = (expr ?? '').trim().split(/\s+/);
  const other = fallback ?? (expr ? `${expr} (IST)` : '');
  if (f.length !== 5) return other;
  const [minute, hour, dom, month, dow] = f as [string, string, string, string, string];
  if (month !== '*') return other;
  const num = (s: string) => (/^\d+$/.test(s) ? Number(s) : null);
  const step = (s: string) => (s.startsWith('*/') ? num(s.slice(2)) : null);
  const m = num(minute);
  const h = num(hour);
  const at = () => clock(h ?? 0, m ?? 0);

  if (dom === '*' && dow === '*' && hour === '*' && minute === '*') return 'Every minute';
  if (dom === '*' && dow === '*' && hour === '*') {
    const n = step(minute);
    if (n !== null) return `Every ${n} minutes`;
    if (m !== null) return `Every hour at :${pad(m)}`;
    return other;
  }
  if (dom === '*' && dow === '*' && m !== null) {
    const n = step(hour);
    if (n !== null) return `Every ${n} hours at :${pad(m)}`;
    if (h !== null) return `Every day at ${at()}`;
    return other;
  }
  if (dom === '*' && m !== null && h !== null) {
    const d = num(dow);
    if (d !== null && d <= 7) return `Every ${DAYS[d % 7]} at ${at()}`;
    return other;
  }
  if (dow === '*' && m !== null && h !== null) {
    const d = num(dom);
    if (d !== null) return `Monthly on day ${d} at ${at()}`;
  }
  return other;
}

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

const pad = (n: number) => String(n).padStart(2, '0');

/** 0:30 → "12:30 am", 6:00 → "6:00 am", 13:05 → "1:05 pm". */
export function clock(h: number, m: number): string {
  const twelve = h % 12 === 0 ? 12 : h % 12;
  return `${twelve}:${pad(m)} ${h < 12 ? 'am' : 'pm'}`;
}
