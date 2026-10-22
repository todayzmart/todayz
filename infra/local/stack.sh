#!/usr/bin/env bash
# stack.sh: the Docker + Caddy half of `make dev` (W0-02). mk/10-stack.mk calls it.
#
#   stack.sh pull           pull every image (make setup)
#   stack.sh up             compose up --wait, then make sure roles and databases exist
#   stack.sh buckets        create buckets and policies, write health probes
#   stack.sh tiles          upload infra/local/pmtiles/*.pmtiles to the tiles bucket if missing
#   stack.sh caddy-start    start Caddy, or reload it when ours is already running
#   stack.sh caddy-stop     stop our Caddy
#   stack.sh stop           stop the containers (data volumes stay)
#   stack.sh logs           follow container logs and .cache/dev/*.log
#   stack.sh reset-db       drop and recreate the todayz and todayz_test databases
#
# Only the compose project `todayz` is touched; nothing here removes volumes.
# One stack runs per machine (fixed ports): up and reset-db refuse to drive a
# stack another checkout started, unless TODAYZ_STACK_FORCE=1.
set -euo pipefail
# shellcheck source=infra/local/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

CADDYFILE="$TZ_LOCAL/Caddyfile"
CADDY_PID="$TZ_DEV/caddy.pid"
SERVICES=(postgres minio lgtm postgres-exporter)
# service:host ports it publishes
SERVICE_PORTS=("postgres:5433" "minio:9100 9101" "lgtm:3300 4317 4318 9390" "postgres-exporter:9187")

die() { echo "stack: $*" >&2; exit 1; }

need_docker() {
  command -v docker >/dev/null || die "docker CLI not found (brew install docker docker-compose colima)"
  if ! docker info >/dev/null 2>&1; then
    die "Docker is not running. Start it with 'colima start' (see infra/local/README.md), then run make dev again"
  fi
}

running_services() { tz_compose ps --status running --services 2>/dev/null || true; }

cmd_pull() {
  if ! command -v docker >/dev/null || ! docker info >/dev/null 2>&1; then
    echo "stack: WARNING: Docker is not running ('colima start'); images will be pulled by make dev" >&2
    return 0
  fi
  tz_compose --profile tools pull --quiet
  echo "stack: images pulled"
}

cmd_up() {
  need_docker
  tz_stack_owner_ok || exit 1
  mkdir -p "$TZ_DEV"
  # Refuse early, with the owner, when a port we publish is taken by someone else.
  local running entry svc port owner bad=0
  running=" $(running_services | tr '\n' ' ') "
  for entry in "${SERVICE_PORTS[@]}"; do
    svc="${entry%%:*}"
    [[ "$running" == *" $svc "* ]] && continue
    for port in ${entry#*:}; do
      owner="$(tz_port_owner "$port")"
      if [[ -n "$owner" ]]; then
        echo "stack: port $port (for $svc) is already in use by $owner" >&2
        bad=1
      fi
    done
  done
  [[ $bad -eq 0 ]] || die "free the port(s) above (make stop, or stop the other program) and run make dev again"

  tz_compose up -d ${TZ_PULL[@]+"${TZ_PULL[@]}"} --wait --wait-timeout 180 "${SERVICES[@]}"
  ensure_db
  echo "stack: containers healthy (${SERVICES[*]})"
}

# init.sql only runs on an empty volume; running it again is safe and covers old volumes.
ensure_db() {
  tz_compose exec -T -e PGOPTIONS="-c client_min_messages=warning" postgres psql -X -q -U todayz -d postgres -v ON_ERROR_STOP=1 \
    -f /docker-entrypoint-initdb.d/10-todayz.sql >/dev/null
}

cmd_buckets() {
  need_docker
  tz_compose --progress quiet run --rm --no-deps ${TZ_PULL[@]+"${TZ_PULL[@]}"} minio-init buckets
}

cmd_tiles() {
  need_docker
  local f any=0
  for f in "$TZ_LOCAL"/pmtiles/*.pmtiles; do [[ -e "$f" ]] && any=1; done
  if [[ $any -eq 0 ]]; then
    echo "stack: WARNING: no map tiles in infra/local/pmtiles (run make setup); maps stay blank" >&2
    return 0
  fi
  tz_compose --progress quiet run --rm --no-deps ${TZ_PULL[@]+"${TZ_PULL[@]}"} minio-init tiles
}

# Our Caddy: the pid caddy wrote to .cache/dev/caddy.pid, still alive, and still
# a process named caddy (a recycled pid is never signalled).
caddy_pid() {
  local pid
  [[ -f "$CADDY_PID" ]] || return 1
  pid="$(head -n 1 "$CADDY_PID" | tr -d '[:space:]')"
  tz_valid_pid "$pid" || return 1
  [[ "$(basename "$(ps -o comm= -p "$pid" 2>/dev/null || true)")" == caddy ]] || return 1
  echo "$pid"
}
caddy_ours_running() { caddy_pid >/dev/null; }

cmd_caddy_start() {
  command -v caddy >/dev/null || die "caddy not found (brew install caddy)"
  mkdir -p "$TZ_DEV"
  export TODAYZ_ROOT="$TZ_ROOT"
  if caddy_ours_running; then
    caddy reload --config "$CADDYFILE" --adapter caddyfile --address "$TZ_CADDY_ADMIN" >/dev/null 2>"$TZ_DEV/caddy-cli.log" ||
      die "caddy reload failed; see .cache/dev/caddy-cli.log"
    echo "stack: caddy reloaded"
    return 0
  fi
  local port owner
  for port in 443 80; do
    owner="$(tz_port_owner "$port")"
    [[ -z "$owner" ]] || die "port $port is in use by $owner; stop it (or 'caddy stop' if it is an old Caddy) and run make dev again"
  done
  if tz_curl -s -m 2 -o /dev/null "http://$TZ_CADDY_ADMIN/config/" 2>/dev/null; then
    die "another Caddy answers on its admin port $TZ_CADDY_ADMIN; stop it with 'caddy stop' first"
  fi
  caddy start --config "$CADDYFILE" --adapter caddyfile --pidfile "$CADDY_PID" >/dev/null 2>"$TZ_DEV/caddy-cli.log" ||
    die "caddy failed to start; see .cache/dev/caddy-cli.log and .cache/dev/caddy.log"
  echo "stack: caddy started on :443 and :80 (*.todayz.localhost)"
  if ! security find-certificate -c "Caddy Local Authority" >/dev/null 2>&1; then
    echo "stack: note: browsers will warn about the certificate until you run 'caddy trust' once (asks for your password)"
  fi
}

cmd_caddy_stop() {
  local pid
  if pid="$(caddy_pid)"; then
    caddy stop --address "$TZ_CADDY_ADMIN" >/dev/null 2>&1 || kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 25); do caddy_ours_running || break; sleep 0.2; done
    echo "stack: caddy stopped"
  else
    echo "stack: caddy not running"
  fi
  rm -f "$CADDY_PID"
}

cmd_stop() {
  if ! command -v docker >/dev/null || ! docker info >/dev/null 2>&1; then
    echo "stack: Docker is not running; no containers to stop"
    return 0
  fi
  tz_compose stop
  echo "stack: containers stopped (data volumes kept)"
}

cmd_logs() {
  need_docker
  tz_compose logs -f --tail=50 &
  local compose_pid=$!
  trap 'kill "$compose_pid" 2>/dev/null || true' EXIT INT TERM
  local files=() f
  for f in "$TZ_DEV"/*.log; do [[ -e "$f" ]] && files+=("$f"); done
  if [[ ${#files[@]} -gt 0 ]]; then
    tail -n 50 -F "${files[@]}"
  else
    wait "$compose_pid"
  fi
}

cmd_reset_db() {
  need_docker
  tz_stack_owner_ok || exit 1
  if [[ " $(running_services | tr '\n' ' ') " != *" postgres "* ]]; then
    tz_compose up -d ${TZ_PULL[@]+"${TZ_PULL[@]}"} --wait --wait-timeout 120 postgres
  fi
  tz_compose exec -T -e PGOPTIONS="-c client_min_messages=warning" postgres psql -X -q -U todayz -d postgres -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
DROP DATABASE IF EXISTS todayz WITH (FORCE);
DROP DATABASE IF EXISTS todayz_test WITH (FORCE);
SQL
  ensure_db
  echo "stack: databases todayz and todayz_test recreated (empty; migrations and seed run next)"
}

case "${1:-}" in
  pull) cmd_pull ;;
  up) cmd_up ;;
  buckets) cmd_buckets ;;
  tiles) cmd_tiles ;;
  caddy-start) cmd_caddy_start ;;
  caddy-stop) cmd_caddy_stop ;;
  stop) cmd_stop ;;
  logs) cmd_logs ;;
  reset-db) cmd_reset_db ;;
  *) sed -n '2,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 2 ;;
esac
