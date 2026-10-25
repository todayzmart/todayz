export interface Change {
  key: string;
  before: string | null;
  after: string | null;
}

const show = (v: unknown): string | null => (v === undefined ? null : typeof v === 'string' ? v : JSON.stringify(v));

/** The fields an audit row changed, in key order: a field only in `after` was added, only in `before` removed. */
export function changes(before: Record<string, unknown> | null, after: Record<string, unknown> | null): Change[] {
  const b = before ?? {};
  const a = after ?? {};
  const keys = [...new Set([...Object.keys(b), ...Object.keys(a)])].sort();
  const out: Change[] = [];
  for (const key of keys) {
    const x = show(b[key]);
    const y = show(a[key]);
    if (x !== y) out.push({ key, before: x, after: y });
  }
  return out;
}
