#!/usr/bin/env bash
# Samples the local stack every LOAD_SAMPLE_EVERY seconds (default 30) into a CSV, for the soak's
# memory and connection verdict (08 §6: no memory growth, no connection leak):
#
#   sample.sh <out.csv> [seconds]      runs until killed, or for [seconds]
#
# Columns: time, api and worker RSS (MB, ps on the processes listening on 8180 and 8181), the
# api's Go heap and goroutines and its pgx pool (acquired, idle, total) from Prometheus
# (otel-lgtm on 127.0.0.1:9390), and the database's connections by state (pg_stat_activity, via
# DATABASE_URL, which must be the local stack's). Prints numbers only.
set -euo pipefail

out="${1:?usage: sample.sh <out.csv> [seconds]}"
limit="${2:-0}"
every="${LOAD_SAMPLE_EVERY:-30}"
prom="${LOAD_PROM_URL:-http://127.0.0.1:9390}"
case "$prom" in http://127.0.0.1:* | http://localhost:*) ;; *) echo "sample.sh: LOAD_PROM_URL must be loopback" >&2; exit 2 ;; esac
case "${DATABASE_URL:-}" in
  *@127.0.0.1:5433/* | *@localhost:5433/*) ;;
  *) echo "sample.sh: DATABASE_URL must be the local stack (127.0.0.1:5433)" >&2; exit 2 ;;
esac

rss_mb() {
  local pid
  pid="$(lsof -nP -t -iTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -1 || true)"
  if [ -z "$pid" ]; then echo ""; return; fi
  ps -o rss= -p "$pid" 2>/dev/null | awk '{printf "%.1f", $1/1024}'
}

# One instant value of a metric whose name matches the regex (telemetry may add a unit suffix),
# summed over series.
prom_val() {
  curl -fsS --max-time 5 -G "$prom/api/v1/query" --data-urlencode "query=sum({__name__=~\"$1\"})" 2>/dev/null |
    jq -r '.data.result[0].value[1] // ""' 2>/dev/null || echo ""
}

echo "time,api_rss_mb,worker_rss_mb,go_heap_mb,goroutines,pool_acquired,pool_idle,pool_total,pg_active,pg_idle,pg_idle_in_tx,pg_total" >"$out"
start="$(date +%s)"
while :; do
  heap="$(prom_val 'tz_go_heap_alloc_bytes.*')"
  [ -n "$heap" ] && heap="$(awk -v b="$heap" 'BEGIN{printf "%.1f", b/1048576}')"
  pg="$(psql "$DATABASE_URL" -X -At -F, -c "select count(*) filter (where state='active'), count(*) filter (where state='idle'), count(*) filter (where state like 'idle in transaction%'), count(*) from pg_stat_activity where datname = current_database() and backend_type = 'client backend'" 2>/dev/null || echo ",,,")"
  printf '%s,%s,%s,%s,%s,%s,%s,%s,%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(rss_mb 8180)" "$(rss_mb 8181)" "$heap" \
    "$(prom_val 'tz_go_goroutines.*')" "$(prom_val 'tz_db_pool_acquired_connections.*')" \
    "$(prom_val 'tz_db_pool_idle_connections.*')" "$(prom_val 'tz_db_pool_total_connections.*')" "$pg" >>"$out"
  if [ "$limit" -gt 0 ] && [ $(( $(date +%s) - start )) -ge "$limit" ]; then break; fi
  sleep "$every"
done
