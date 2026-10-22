#!/usr/bin/env bash
# health.sh: health of the local stack (W0-02).
#
#   health.sh [--wait SEC] [--no-html]
#
# Checks Postgres, MinIO, Grafana LGTM, postgres_exporter, Caddy (HTTPS for
# every host) and every process in the proc.sh registry, then prints a table:
#   ok    green  healthy
#   FAIL  red    required and down (or registered with proc.sh and down)
#   --    grey   not registered / not running yet (not an error)
# Exits 1 when anything is red. --wait retries for up to SEC seconds while
# something is red (make dev uses it right after `compose up`).
# Writes .cache/dev/health.html, served by Caddy at https://todayz.localhost/_health.
# Prints names, ports and states only, never env values.
set -euo pipefail
# shellcheck source=infra/local/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

WAIT=0
HTML=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --wait) WAIT="$2"; shift 2 ;;
    --no-html) HTML=0; shift ;;
    *) sed -n '2,14p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
  esac
done
[[ "$WAIT" =~ ^[0-9]{1,4}$ ]] || { echo "health: --wait (or TODAYZ_HEALTH_WAIT) must be a number of seconds" >&2; exit 2; }

mkdir -p "$TZ_DEV"

# Upstreams other work packages run through proc.sh, keyed by port.
# host label | port | Caddy host
APP_UPSTREAMS=(
  "api|8180|api.todayz.localhost"
  "customer app|5301|app.todayz.localhost"
  "business app|5302|business.todayz.localhost"
  "admin app|5303|admin.todayz.localhost"
  "site|5304|todayz.localhost"
  "mocks|4010|mocks.todayz.localhost"
)

ROWS=()      # group|name|state|detail ; state: ok fail skip
row() { ROWS+=("$1|$2|$3|$4"); }

http_code() { # url [curl args...]
  local url="$1"; shift
  tz_curl -s -o /dev/null -m 5 -w '%{http_code}' "$@" "$url" 2>/dev/null || true
}

# --- Postgres -----------------------------------------------------------------
check_postgres() {
  if ! tz_port_open 5433; then
    row postgres "postgres :5433" fail "port closed (is the stack up? make dev)"
    return
  fi
  local out
  out="$(tz_compose exec -T postgres pg_isready -U todayz -d postgres 2>&1)" ||
    { row postgres "postgres :5433" fail "pg_isready: not accepting connections"; return; }
  row postgres "postgres :5433" ok "accepting connections"

  # One round trip in the app database (init.sql creates roles and extensions).
  local sql="
SELECT
  (SELECT count(*) FROM pg_database WHERE datname IN ('todayz', 'todayz_test')),
  (SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()),
  postgis_lib_version(),
  current_setting('shared_preload_libraries') LIKE '%pg_stat_statements%',
  (SELECT count(*) FROM pg_stat_statements) >= 0,
  (SELECT count(*) FROM pg_extension WHERE extname IN ('postgis', 'pg_trgm', 'unaccent', 'pg_stat_statements', 'pgcrypto')),
  (SELECT count(*) FROM pg_roles WHERE rolname IN ('todayz_owner', 'todayz_app', 'grafana_ro', 'todayz_exporter') AND NOT rolsuper);"
  out="$(tz_compose exec -T postgres psql -X -q -U todayz -d todayz -At -F '|' -v ON_ERROR_STOP=1 <<<"$sql" 2>&1)" || true
  local dbs owner postgis preload pss_ok exts roles
  IFS='|' read -r dbs owner postgis preload pss_ok exts roles <<<"$(grep '|' <<<"$out" | tail -n 1)"
  if [[ "${dbs:-}" == 2 && "${owner:-}" == todayz_owner ]]; then row postgres "databases" ok "todayz, todayz_test (owner todayz_owner)"
  else row postgres "databases" fail "todayz and todayz_test owned by todayz_owner expected (make reset)"; fi
  if [[ "${exts:-}" == 5 && -n "${postgis:-}" ]]; then row postgres "extensions" ok "PostGIS $postgis, pg_trgm, unaccent, pgcrypto, pg_stat_statements"
  else row postgres "extensions" fail "missing in todayz (infra/local/postgres/init.sql)"; fi
  if [[ "${preload:-}" == t && "${pss_ok:-}" == t ]]; then
    row postgres "pg_stat_statements" ok "preloaded and collecting"
  else row postgres "pg_stat_statements" fail "not preloaded or extension missing"; fi
  if [[ "${roles:-}" == 4 ]]; then row postgres "roles" ok "todayz_owner, todayz_app, grafana_ro, todayz_exporter (none superuser)"
  else row postgres "roles" fail "missing or superuser (infra/local/postgres/init.sql)"; fi
}

# --- MinIO ---------------------------------------------------------------------
check_minio() {
  local code
  code="$(http_code http://127.0.0.1:9100/minio/health/live)"
  if [[ "$code" != 200 ]]; then
    row minio "minio :9100" fail "health/live answered ${code:-nothing}"
    return
  fi
  row minio "minio :9100" ok "live"
  code="$(http_code http://127.0.0.1:9101/)"
  if [[ "$code" =~ ^[23] ]]; then row minio "minio console :9101" ok "HTTP $code"
  else row minio "minio console :9101" fail "answered ${code:-nothing}"; fi

  # Anonymous reads of the probe objects make-dev writes prove bucket + policy.
  local b want
  for b in images:200 tiles:200 private:403; do
    want="${b#*:}"; b="${b%%:*}"
    code="$(http_code "http://127.0.0.1:9100/$b/_health/probe.txt")"
    if [[ "$code" == "$want" ]]; then
      if [[ "$want" != 200 ]]; then row minio "bucket $b" ok "exists, no anonymous access"
      elif [[ "$(http_code "http://127.0.0.1:9100/$b/")" == 200 ]]; then row minio "bucket $b" fail "anonymous listing is allowed (make dev resets the policy)"
      else row minio "bucket $b" ok "exists, anonymous read, no listing"; fi
    elif [[ "$code" == 404 ]]; then
      row minio "bucket $b" fail "missing (make dev creates it)"
    else
      row minio "bucket $b" fail "anonymous GET gave ${code:-nothing}, want $want"
    fi
  done

  local f n missing=() have=0
  for f in "$TZ_LOCAL"/pmtiles/*.pmtiles; do
    [[ -e "$f" ]] || continue
    n="$(basename "$f")"; have=$((have + 1))
    [[ "$(http_code "http://127.0.0.1:9100/tiles/$n" -I)" == 200 ]] || missing+=("$n")
  done
  if [[ $have -eq 0 ]]; then
    row minio "tiles uploaded" skip "no local archives (make setup downloads them)"
  elif [[ ${#missing[@]} -eq 0 ]]; then
    row minio "tiles uploaded" ok "$have archive(s) in the tiles bucket"
  else
    row minio "tiles uploaded" fail "not in bucket: ${missing[*]}"
  fi
}

# --- Grafana LGTM and the exporter -----------------------------------------------
# Grafana enforces its domain, so direct requests carry the Caddy host name.
grafana() { local p="$1"; shift; tz_curl -s -H 'Host: grafana.todayz.localhost' "$@" "http://127.0.0.1:3300$p" 2>/dev/null || true; }

check_lgtm() {
  local body code
  body="$(grafana /api/health -m 5)"
  if [[ "$(jq -r '.database // empty' <<<"$body" 2>/dev/null)" == ok ]]; then
    row lgtm "grafana :3300" ok "api/health ok, version $(jq -r '.version' <<<"$body")"
  else
    row lgtm "grafana :3300" fail "api/health not ok"
  fi
  body="$(grafana /api/datasources/uid/todayz-postgres/health -m 8)"
  if [[ "$(jq -r '.status // empty' <<<"$body" 2>/dev/null)" == OK ]]; then
    row lgtm "grafana postgres datasource" ok "grafana_ro connects (read-only)"
  else
    row lgtm "grafana postgres datasource" fail "health: $(jq -r '.message // "no answer"' <<<"$body" 2>/dev/null | head -c 80)"
  fi
  code="$(http_code http://127.0.0.1:4318/v1/traces -X POST -H 'Content-Type: application/json' --data '{"resourceSpans":[]}')"
  if [[ "$code" == 200 ]]; then row lgtm "otlp http :4318" ok "accepts traces"
  else row lgtm "otlp http :4318" fail "POST /v1/traces gave ${code:-nothing}"; fi
  if tz_port_open 4317; then row lgtm "otlp grpc :4317" ok "listening"
  else row lgtm "otlp grpc :4317" fail "port closed"; fi

  body="$(tz_curl -s -m 5 http://127.0.0.1:9187/metrics 2>/dev/null || true)"
  if grep -q '^pg_up 1' <<<"$body"; then row lgtm "postgres_exporter :9187" ok "pg_up 1"
  elif [[ -n "$body" ]]; then row lgtm "postgres_exporter :9187" fail "pg_up is not 1"
  else row lgtm "postgres_exporter :9187" fail "no /metrics"; fi

  body="$(tz_curl -s -m 5 -G http://127.0.0.1:9390/api/v1/query --data-urlencode 'query=up{job="postgres-exporter"}' 2>/dev/null || true)"
  if [[ "$(jq -r '.data.result[0].value[1] // empty' <<<"$body" 2>/dev/null)" == 1 ]]; then
    row lgtm "prometheus :9390" ok "scrapes postgres-exporter"
  else
    row lgtm "prometheus :9390" fail "postgres-exporter target not up yet"
  fi
}

# --- Caddy ---------------------------------------------------------------------------
registered_port() { awk -F '\t' -v p="$1" '$2 == p { found = 1 } END { exit !found }' "$TZ_REGISTRY" 2>/dev/null; }

check_caddy() {
  if ! tz_curl -s -m 3 -o /dev/null "http://$TZ_CADDY_ADMIN/config/" 2>/dev/null; then
    row caddy "caddy" fail "admin API $TZ_CADDY_ADMIN not answering (make dev starts it)"
    for h in api app business admin mocks grafana s3 img tiles; do row caddy "https://$h.todayz.localhost" skip "caddy down"; done
    return
  fi
  if [[ -f "$TZ_CADDY_ROOT_CERT" ]]; then row caddy "caddy" ok "running, local CA present"
  else row caddy "caddy" ok "running (local CA not created yet)"; fi

  local code
  # HTTP only redirects.
  code="$(http_code http://app.todayz.localhost/)"
  if [[ "$code" == 308 ]]; then row caddy "http :80" ok "redirects to https"
  else row caddy "http :80" fail "answered ${code:-nothing}, want 308"; fi

  # Stack upstreams: must answer through Caddy.
  local spec host path want args
  for spec in \
    "grafana.todayz.localhost|/api/health|200|" \
    "s3.todayz.localhost|/minio/health/live|200|" \
    "img.todayz.localhost|/_health/probe.txt|200|" \
    "tiles.todayz.localhost|/_health/probe.txt|206|range" \
    "todayz.localhost|/_health/bundle.css|200|"; do
    IFS='|' read -r host path want args <<<"$spec"
    if [[ "$args" == range ]]; then code="$(http_code "https://$host$path" -H 'Range: bytes=0-3')"
    else code="$(http_code "https://$host$path")"; fi
    if [[ "$code" == "$want" ]]; then row caddy "https://$host" ok "$path -> $code"
    elif [[ "$code" == 000 || -z "$code" ]]; then row caddy "https://$host" fail "TLS or connection failed"
    else row caddy "https://$host" fail "$path -> $code, want $want"; fi
  done

  # App upstreams: Caddy must answer; a missing upstream is grey until its WP runs it.
  local entry label port
  for entry in "${APP_UPSTREAMS[@]}"; do
    IFS='|' read -r label port host <<<"$entry"
    code="$(http_code "https://$host/")"
    if [[ "$code" == 000 || -z "$code" ]]; then
      row caddy "https://$host" fail "TLS or connection failed"
    elif [[ "$code" =~ ^50[234]$ ]]; then
      if registered_port "$port" || tz_port_open "$port"; then
        row caddy "https://$host" fail "$label upstream :$port answers $code"
      else
        row caddy "https://$host" skip "TLS ok; $label (:$port) not running yet"
      fi
    else
      row caddy "https://$host" ok "-> $label :$port ($code)"
    fi
  done
}

# --- proc.sh registry ----------------------------------------------------------------
check_procs() {
  local name port health pid code seen=" "
  if [[ -s "$TZ_REGISTRY" ]]; then
    while IFS=$'\t' read -r name port health pid _; do
      [[ -n "$name" ]] || continue
      seen+="$port "
      # Same rule as proc.sh: the pidfile's pid must be alive with its recorded start time.
      if ! [[ "$name" =~ ^[a-z0-9][a-z0-9_.-]*$ ]] || ! tz_pidfile_alive "$TZ_DEV/pids/$name.pid"; then
        row procs "$name" fail "registered but not running (pid $pid); see .cache/dev/$name.log"
      elif [[ "$health" == "-" ]]; then
        row procs "$name" ok "running (pid $pid${port:+, port $port}); no health URL"
      else
        code="$(http_code "$health")"
        if [[ "$code" =~ ^[23] ]]; then row procs "$name" ok "$health -> $code"
        else row procs "$name" fail "$health -> ${code:-no answer}"; fi
      fi
    done <"$TZ_REGISTRY"
  fi
  local entry label host
  for entry in "${APP_UPSTREAMS[@]}"; do
    IFS='|' read -r label port host <<<"$entry"
    [[ "$seen" == *" $port "* ]] || row procs "$label" skip "not registered (port $port)"
  done
}

run_checks() {
  ROWS=()
  check_postgres
  check_minio
  check_lgtm
  check_caddy
  check_procs
}

count_fail() { local r n=0; for r in "${ROWS[@]}"; do [[ "$r" == *"|fail|"* ]] && n=$((n + 1)); done; echo "$n"; }

# --- Output ------------------------------------------------------------------------------
print_table() {
  local g="" r="" d="" x=""
  if tz_color; then g=$'\033[32m'; r=$'\033[31m'; d=$'\033[90m'; x=$'\033[0m'; fi
  printf '%-10s %-34s %-5s %s\n' GROUP CHECK STATE DETAIL
  local row group name state detail label color
  for row in "${ROWS[@]}"; do
    IFS='|' read -r group name state detail <<<"$row"
    case "$state" in
      ok) label=ok; color="$g" ;;
      fail) label=FAIL; color="$r" ;;
      *) label=--; color="$d" ;;
    esac
    printf '%-10s %-34s %s%-5s%s %s\n' "$group" "$name" "$color" "$label" "$x" "$detail"
  done
}

esc() { local s="$1"; s="${s//&/&amp;}"; s="${s//</&lt;}"; s="${s//>/&gt;}"; printf '%s' "$s"; }

write_assets() {
  # Design-system styles next to the page. The Google Fonts @import is dropped:
  # the page must not reach the internet, so it falls back to system fonts.
  grep -v '^@import' "$TZ_ROOT/design/source/components/bundle.css" >"$TZ_DEV/bundle.css.tmp"
  mv -f "$TZ_DEV/bundle.css.tmp" "$TZ_DEV/bundle.css"
  jq -r '
    def val: if (.value | type) == "object" then (.value.light // (.value | to_entries[0].value)) else .value end;
    "/* Generated by infra/local/health.sh from design/source/tokens.json (light theme). */",
    ":root {",
    ( [.color, .spacing, .radius, .shadow, .size, .zIndex][] | select(. != null) | .tokens[] | "  --\(.name): \(val);" ),
    "  --font-sans: \(.type.families.sans);",
    "  --font-display: \(.type.families.display);",
    "}"' "$TZ_ROOT/design/source/tokens.json" >"$TZ_DEV/tokens.css.tmp"
  mv -f "$TZ_DEV/tokens.css.tmp" "$TZ_DEV/tokens.css"
}

write_html() {
  local fails="$1" now; now="$(date '+%Y-%m-%d %H:%M:%S %Z')"
  local summary pill
  if [[ "$fails" -eq 0 ]]; then summary="All required checks pass"; pill="tz-pill tz-pill-completed"
  else summary="$fails required check(s) failing"; pill="tz-pill tz-pill-rejected"; fi
  {
    cat <<HTML
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Stack health</title>
<link rel="stylesheet" href="/_health/tokens.css">
<link rel="stylesheet" href="/_health/bundle.css">
<style>
  body { background: var(--bg); color: var(--ink); }
  .hz-band { background: var(--wash); border-bottom: 1px solid var(--wash-line); }
  .hz-wrap { max-width: 1040px; margin: 0 auto; padding: var(--space-5) var(--space-4); }
  .hz-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); flex-wrap: wrap; }
  .hz-group td { background: var(--surface-subtle); color: var(--ink-2); font-weight: 600; height: 32px; }
  .hz-detail { white-space: normal !important; color: var(--ink-2); }
  .hz-note { color: var(--ink-2); font-size: 13px; line-height: 18px; margin: var(--space-3) 0 0; }
  code { font-size: 12px; }
</style>
</head>
<body>
<div class="hz-band"><div class="hz-wrap hz-head">
  <div>
    <h1 class="tz-page-title">Local stack health</h1>
    <p class="tz-page-sub">Checked $(esc "$now") by <code>infra/local/health.sh</code></p>
  </div>
  <span class="$pill">$(esc "$summary")</span>
</div></div>
<div class="hz-wrap">
<div class="tz-table-wrap">
<table class="tz-table">
<thead><tr><th>Check</th><th>State</th><th>Detail</th></tr></thead>
<tbody>
HTML
    local row group name state detail last="" cls label
    for row in "${ROWS[@]}"; do
      IFS='|' read -r group name state detail <<<"$row"
      if [[ "$group" != "$last" ]]; then
        printf '<tr class="hz-group"><td colspan="3">%s</td></tr>\n' "$(esc "$group")"
        last="$group"
      fi
      case "$state" in
        ok) cls="tz-pill tz-pill-completed"; label="ok" ;;
        fail) cls="tz-pill tz-pill-rejected"; label="down" ;;
        *) cls="tz-pill"; label="not running" ;;
      esac
      printf '<tr><td>%s</td><td><span class="%s">%s</span></td><td class="hz-detail">%s</td></tr>\n' \
        "$(esc "$name")" "$cls" "$label" "$(esc "$detail")"
    done
    cat <<HTML
</tbody>
</table>
</div>
<p class="hz-note">This page is a snapshot. <code>make dev</code> refreshes it; so does <code>infra/local/health.sh</code>. Grey rows are services another work package has not started yet.</p>
</div>
</body>
</html>
HTML
  } >"$TZ_DEV/health.html.tmp"
  mv -f "$TZ_DEV/health.html.tmp" "$TZ_DEV/health.html"
}

# Assets first: the Caddy check for /_health/bundle.css needs them.
[[ $HTML -eq 1 ]] && write_assets

start=$SECONDS
while :; do
  run_checks
  fails="$(count_fail)"
  [[ "$fails" -eq 0 || $((SECONDS - start)) -ge "$WAIT" ]] && break
  sleep 3
done

print_table
[[ $HTML -eq 1 ]] && write_html "$fails"
echo
if [[ "$fails" -eq 0 ]]; then
  echo "health: all required checks pass (page: https://todayz.localhost/_health)"
else
  echo "health: $fails check(s) failing (page: https://todayz.localhost/_health)" >&2
  exit 1
fi
