/**
 * Grafana Explore links for a run (A-21): its log lines in Loki by `run_id` and its trace in Tempo.
 * The datasource uids are otel-lgtm's (`loki`, `tempo`), the same the dashboards in
 * infra/grafana/dashboards use. The time range is the run's, with a few minutes either side.
 */

const MARGIN_MS = 5 * 60_000;

export interface RunWindow {
  createdAt: string;
  startedAt?: string | null | undefined;
  finishedAt?: string | null | undefined;
}

function range(w: RunWindow, now: string) {
  const start = Date.parse(w.startedAt ?? w.createdAt);
  const end = Date.parse(w.finishedAt ?? now);
  return { from: String(start - MARGIN_MS), to: String(Math.max(end, start) + MARGIN_MS) };
}

function explore(base: string, datasource: string, query: Record<string, string>, w: RunWindow, now: string): string {
  const panes = { a: { datasource, queries: [{ refId: 'A', datasource: { type: datasource, uid: datasource }, ...query }], range: range(w, now) } };
  return `${base}/explore?schemaVersion=1&orgId=1&panes=${encodeURIComponent(JSON.stringify(panes))}`;
}

/** The run's log lines (`logsQuery` comes from the API). Null without a Grafana URL. */
export function logsLink(base: string, logsQuery: string, w: RunWindow, now: string): string | null {
  return base ? explore(base, 'loki', { expr: logsQuery }, w, now) : null;
}

/** The run's trace. Null without a Grafana URL or a trace id. */
export function traceLink(base: string, traceId: string | null | undefined, w: RunWindow, now: string): string | null {
  return base && traceId ? explore(base, 'tempo', { queryType: 'traceql', query: traceId }, w, now) : null;
}
